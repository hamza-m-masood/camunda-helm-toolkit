package capacityplan_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/capacityplan"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

// This is the self-check the feature was scoped to require: run this package's own
// generated output back through the tool's own rules (the same ones a customer's
// `check` run would apply) across a spread of inputs, and confirm it never
// recommends a configuration its own tool would flag.
func TestRecommend_NeverTripsItsOwnRules(t *testing.T) {
	cases := []capacityplan.Input{
		{ThroughputPerSec: 10, AvgPayloadKB: 1, RetentionDays: 1},
		{ThroughputPerSec: 150, AvgPayloadKB: 2, RetentionDays: 7},
		{ThroughputPerSec: 500, AvgPayloadKB: 5, RetentionDays: 7},
		{ThroughputPerSec: 2000, AvgPayloadKB: 10, RetentionDays: 30},
		{ThroughputPerSec: 10000, AvgPayloadKB: 20, RetentionDays: 90},
		{ThroughputPerSec: 0.5, AvgPayloadKB: 0.1, RetentionDays: 0}, // near-zero, default retention
	}

	for _, in := range cases {
		rec := capacityplan.Recommend(in)
		v, err := values.ParseYAML([]byte(rec.ToValuesOverlay()))
		if err != nil {
			t.Fatalf("input %+v: overlay did not parse as YAML: %v", in, err)
		}

		if f := rules.CheckReplicationFactor(v); len(f) != 0 {
			t.Errorf("input %+v: CCD007 (replicationFactor > clusterSize) fired on this "+
				"tool's own recommendation: %+v", in, f)
		}
		if f := rules.CheckMemoryBurstable(v); len(f) != 0 {
			t.Errorf("input %+v: CCD002 (Burstable QoS) fired on this tool's own "+
				"recommendation — requests must equal limits: %+v", in, f)
		}

		if rec.ClusterSize < rec.ReplicationFactor {
			t.Errorf("input %+v: clusterSize (%d) < replicationFactor (%d)", in, rec.ClusterSize, rec.ReplicationFactor)
		}
		if rec.PartitionCount < rec.ClusterSize {
			t.Errorf("input %+v: partitionCount (%d) < clusterSize (%d)", in, rec.PartitionCount, rec.ClusterSize)
		}
		if rec.PVCSizeGiB < 32 {
			t.Errorf("input %+v: pvcSize %dGi below the chart's own 32Gi default floor", in, rec.PVCSizeGiB)
		}
		if len(rec.Reasoning) == 0 {
			t.Errorf("input %+v: expected non-empty reasoning for every recommendation", in)
		}
	}
}

func TestToValuesOverlay_ParsesAsValidYAML(t *testing.T) {
	rec := capacityplan.Recommend(capacityplan.Input{ThroughputPerSec: 300, AvgPayloadKB: 4, RetentionDays: 14})
	v, err := values.ParseYAML([]byte(rec.ToValuesOverlay()))
	if err != nil {
		t.Fatal(err)
	}
	rf, ok := values.GetPath(v, "orchestration.replicationFactor")
	if !ok || rf != "3" {
		t.Errorf("orchestration.replicationFactor = %v (ok=%v), want \"3\"", rf, ok)
	}
	reqMem, ok := values.GetPath(v, "orchestration.resources.requests.memory")
	limMem, ok2 := values.GetPath(v, "orchestration.resources.limits.memory")
	if !ok || !ok2 || reqMem != limMem {
		t.Errorf("requests.memory (%v) != limits.memory (%v) — must always be equal", reqMem, limMem)
	}
}
