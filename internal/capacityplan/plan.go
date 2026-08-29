// Package capacityplan produces a STARTING-POINT sizing recommendation for a Zeebe
// cluster from a throughput/payload estimate. It is a heuristic, not an authoritative
// Camunda-certified formula — this tool has no access to Camunda's internal benchmark
// data, and every number it prints comes with the arithmetic that produced it so an
// operator can judge and adjust it rather than take it on faith.
package capacityplan

import (
	"fmt"
	"math"
)

// Input is what the operator actually knows or estimates about their workload.
type Input struct {
	ThroughputPerSec float64 // commands/sec, sustained
	AvgPayloadKB     float64 // average command payload size
	RetentionDays    int     // how long exported records need to stay queryable; 0 -> defaults to 7
}

// Recommendation is one sized value plus the arithmetic that produced it, so the
// reasoning is as visible as the number.
type Recommendation struct {
	ReplicationFactor int
	ClusterSize       int
	PartitionCount    int
	PVCSizeGiB        int
	MemoryLimitMiB    int // requests == limits per CCD002 — Guaranteed QoS by default

	Reasoning []string
}

const (
	// baselineBrokers is the smallest cluster this tool will ever recommend — 3
	// is the minimum for any quorum-based replicationFactor of 3 to make sense.
	baselineBrokers = 3
	// brokerScalingStepPerSec adds one broker for every this-many commands/sec
	// above the baseline — an illustrative starting ratio, not a benchmarked one.
	brokerScalingStepPerSec = 500.0
	// partitionsPerSec is an illustrative commands/sec-per-partition ratio used
	// only to pick a starting partitionCount — validate under your own load test.
	partitionsPerSec = 150.0
	// pvcSafetyMultiplier accounts for RocksDB write amplification, snapshots,
	// and headroom above the exporter watermark — not a measured constant.
	pvcSafetyMultiplier = 3.0
	// defaultRetentionDays is used when the operator doesn't specify one.
	defaultRetentionDays = 7
	// bufferDays is how long the broker's OWN PVC needs to hold uncompacted
	// command data if the exporter/secondary storage lags or is briefly
	// unavailable. This is deliberately NOT the same as --retention-days: how
	// long EXPORTED data stays queryable is a secondary-storage (Elasticsearch/
	// OpenSearch) sizing question this tool does not answer — Zeebe's own log
	// is compacted once records are exported, so its disk should not be sized
	// for the full retention window (an earlier version of this formula did
	// exactly that and produced a multi-terabyte-per-broker recommendation for
	// a modest workload; this constant exists specifically to not repeat that).
	bufferDays = 2.0
)

// Recommend produces a starting-point sizing recommendation. It never returns a
// replicationFactor greater than clusterSize — that invariant is enforced here and
// re-checked by an automated test that runs this package's own output back through
// rules.CheckReplicationFactor (CCD007), the same rule a customer's own `check` run
// would apply.
func Recommend(in Input) Recommendation {
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = defaultRetentionDays
	}

	replicationFactor := 3 // fixed: below 3, there is no fault tolerance to plan for

	clusterSize := baselineBrokers
	extraLoad := in.ThroughputPerSec - brokerScalingStepPerSec
	if extraLoad > 0 {
		clusterSize = baselineBrokers + int(math.Ceil(extraLoad/brokerScalingStepPerSec))
	}
	if clusterSize < replicationFactor {
		clusterSize = replicationFactor // invariant: never recommend RF > clusterSize
	}

	rawPartitions := math.Ceil(in.ThroughputPerSec / partitionsPerSec)
	partitionCount := int(math.Max(rawPartitions, float64(clusterSize)))
	// round up to a multiple of clusterSize for even distribution across brokers
	if partitionCount%clusterSize != 0 {
		partitionCount += clusterSize - (partitionCount % clusterSize)
	}

	// Sized for an operational buffer (bufferDays), NOT --retention-days — see
	// the bufferDays doc comment for why those are different questions.
	bytesPerDay := in.ThroughputPerSec * 86400 * in.AvgPayloadKB * 1024
	totalBytes := bytesPerDay * bufferDays * pvcSafetyMultiplier
	pvcPerBrokerGiB := int(math.Ceil(totalBytes / float64(clusterSize) / (1 << 30)))
	if pvcPerBrokerGiB < 32 {
		pvcPerBrokerGiB = 32 // the chart's own shipped default floor
	}

	// Memory: start from the chart's own default limit (3000Mi) and scale
	// lightly with partitions-per-broker, since each partition carries its own
	// RocksDB instance. requests == limits throughout, per CCD002 — the same
	// finding this tool's own `check` command would flag if requests were lower.
	partitionsPerBroker := int(math.Ceil(float64(partitionCount) / float64(clusterSize)))
	memoryLimitMiB := 3000 + (partitionsPerBroker-1)*500
	if memoryLimitMiB < 3000 {
		memoryLimitMiB = 3000
	}

	r := Recommendation{
		ReplicationFactor: replicationFactor,
		ClusterSize:       clusterSize,
		PartitionCount:    partitionCount,
		PVCSizeGiB:        pvcPerBrokerGiB,
		MemoryLimitMiB:    memoryLimitMiB,
	}
	r.Reasoning = []string{
		fmt.Sprintf("replicationFactor = 3 (fixed): below 3, there is no fault tolerance to plan for regardless of load."),
		fmt.Sprintf("clusterSize = %d: baseline of %d brokers, +1 per %.0f commands/sec above %.0f/sec (you asked for %.0f/sec). This is an illustrative ratio, not a benchmarked one — validate under your own load test.",
			clusterSize, baselineBrokers, brokerScalingStepPerSec, brokerScalingStepPerSec, in.ThroughputPerSec),
		fmt.Sprintf("partitionCount = %d: ceil(%.0f/sec ÷ %.0f/sec-per-partition) = %.0f, raised to clusterSize and rounded up to a multiple of it for even distribution.",
			partitionCount, in.ThroughputPerSec, partitionsPerSec, rawPartitions),
		fmt.Sprintf("pvcSize (per broker) = %dGi: %.0f/sec × 86400s × %.1fKB payload × %.0f operational-buffer day(s) × %.0fx safety margin (RocksDB write amplification + snapshots), divided across %d brokers. Floored at the chart's own 32Gi default. This is NOT your --retention-days (%d) — that governs how long EXPORTED data stays queryable in secondary storage (Elasticsearch/OpenSearch), which this tool does not size; the broker's own disk only needs to survive a temporary export lag, not the full retention window.",
			pvcPerBrokerGiB, in.ThroughputPerSec, in.AvgPayloadKB, bufferDays, pvcSafetyMultiplier, clusterSize, retentionDays),
		fmt.Sprintf("memory requests = limits = %dMi (Guaranteed QoS, per this tool's own CCD002 finding): the chart's 3000Mi default, +500Mi per partition-per-broker above 1 (%d partitions ÷ %d brokers ≈ %d per broker).",
			memoryLimitMiB, partitionCount, clusterSize, partitionsPerBroker),
		"This is a heuristic starting point, not a certified sizing — validate under a load test that reflects your real traffic shape before trusting it in production.",
	}
	return r
}
