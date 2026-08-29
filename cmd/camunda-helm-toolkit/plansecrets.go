package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/secretplan"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func runPlanSecrets(args []string) int {
	fs := flag.NewFlagSet("plan-secrets", flag.ContinueOnError)
	release := fs.String("release", "", "installed release name (required)")
	namespace := fs.String("namespace", "default", "kubernetes namespace")
	fs.StringVar(namespace, "n", "default", "kubernetes namespace (shorthand)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *release == "" {
		fmt.Fprintln(os.Stderr, "error: --release is required")
		return 2
	}

	raw, err := live.GetHelmValues(*namespace, *release)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	effective, err := values.ParseYAML(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	secrets, err := live.ListOwnedSecrets(*namespace, *release)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	recs := secretplan.Plan(effective, secrets, *namespace)
	if len(recs) == 0 {
		fmt.Println("No at-risk secrets found — every Secret this tool checked is either " +
			"already pinned via existingSecret or already referenced by name in your values.")
		return 0
	}

	fmt.Printf("%d secret(s) worth reviewing before your next helm upgrade:\n\n", len(recs))
	for _, r := range recs {
		fmt.Printf("--- %s (%s, confidence: %s) ---\n", r.SecretName, r.Label, r.Confidence)
		fmt.Println(r.Note)
		fmt.Println()
		fmt.Println("Back it up first:")
		for _, cmd := range r.BackupCommands {
			fmt.Println("  $ " + cmd)
		}
		if len(r.CreateCommands) > 0 {
			fmt.Println("\nThen create the independent, chart-owned-nothing copy:")
			for _, cmd := range r.CreateCommands {
				fmt.Println("  $ " + cmd)
			}
		}
		if r.OverlayYAML != "" {
			fmt.Println("\nSuggested values overlay:")
			fmt.Println(r.OverlayYAML)
		}
		fmt.Println()
	}
	return 1
}
