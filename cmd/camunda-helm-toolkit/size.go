package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/capacityplan"
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
	// capacityplan.Recommend treats any RetentionDays <= 0 as "not specified, use the
	// default" — correct for callers that never set the field at all, but a user who
	// explicitly typed --retention-days 0 (or a negative number) on this CLI gets the
	// same silent substitution with no indication their input was overridden. flag's
	// own zero value can't tell "omitted" from "explicitly 0" apart on its own, so
	// check which flags were actually set and only reject a BAD explicit value.
	explicitRetentionDays := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "retention-days" {
			explicitRetentionDays = true
		}
	})
	if explicitRetentionDays && *retentionDays <= 0 {
		fmt.Fprintf(os.Stderr, "error: --retention-days must be a positive number of days, got %d (omit the flag entirely to use the default of 7)\n", *retentionDays)
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
