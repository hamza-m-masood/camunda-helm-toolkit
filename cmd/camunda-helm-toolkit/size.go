package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/capacityplan"
)

func runSize(args []string) int {
	fs := flag.NewFlagSet("size", flag.ContinueOnError)
	throughput := fs.Float64("throughput", 0, "sustained commands/sec (required)")
	avgPayloadKB := fs.Float64("avg-payload-kb", 0, "average command payload size in KB (required)")
	retentionDays := fs.Int("retention-days", 0, "how long exported records must stay queryable (default 7)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *throughput <= 0 || *avgPayloadKB <= 0 {
		fmt.Fprintln(os.Stderr, "error: --throughput and --avg-payload-kb are both required and must be > 0")
		return 2
	}

	rec := capacityplan.Recommend(capacityplan.Input{
		ThroughputPerSec: *throughput,
		AvgPayloadKB:     *avgPayloadKB,
		RetentionDays:    *retentionDays,
	})

	fmt.Println("HEURISTIC starting point — not a certified sizing. Validate under a load test")
	fmt.Println("that reflects your real traffic shape before trusting this in production.")
	fmt.Println()
	for _, line := range rec.Reasoning {
		fmt.Println("- " + line)
	}
	fmt.Println()
	fmt.Println("Suggested values overlay:")
	fmt.Print(rec.ToValuesOverlay())
	return 0
}
