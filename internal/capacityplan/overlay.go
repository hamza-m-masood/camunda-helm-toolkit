package capacityplan

import "fmt"

// ToValuesOverlay renders the recommendation as a values.yaml overlay a customer can
// hand straight to `helm install -f`.
func (r Recommendation) ToValuesOverlay() string {
	return fmt.Sprintf(`orchestration:
  replicationFactor: "%d"
  clusterSize: "%d"
  partitionCount: "%d"
  pvcSize: "%dGi"
  resources:
    requests:
      memory: "%dMi"
    limits:
      memory: "%dMi"
`, r.ReplicationFactor, r.ClusterSize, r.PartitionCount, r.PVCSizeGiB, r.MemoryLimitMiB, r.MemoryLimitMiB)
}
