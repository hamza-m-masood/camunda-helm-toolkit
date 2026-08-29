package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/watcher"
)

func runWatchOnce(args []string) int {
	fs := flag.NewFlagSet("watch-once", flag.ContinueOnError)
	release := fs.String("release", "", "installed release name (required)")
	namespace := fs.String("namespace", "default", "kubernetes namespace")
	fs.StringVar(namespace, "n", "default", "kubernetes namespace (shorthand)")
	webhookURL := fs.String("webhook-url", "", "POST new findings here as JSON; if unset, just log them")
	stateName := fs.String("state-configmap", "", "ConfigMap name holding prior-run state (default: <release>-helm-toolkit-state)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *release == "" {
		fmt.Fprintln(os.Stderr, "error: --release is required")
		return 2
	}
	if *stateName == "" {
		*stateName = *release + "-helm-toolkit-state"
	}

	current, err := runLiveFindings(*release, *namespace)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	prevKeys, hadState, err := watcher.LoadState(*namespace, *stateName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error loading prior state:", err)
		return 2
	}
	if !hadState {
		fmt.Println("no prior state found — treating every current finding as new (first run)")
	}

	newFindings := watcher.Diff(current, prevKeys)
	fmt.Printf("%d total finding(s), %d new since last run\n", len(current), len(newFindings))
	for _, f := range newFindings {
		fmt.Printf("  [NEW] [%s] %s (%s)\n", f.Severity, f.Title, f.RuleID)
	}

	if len(newFindings) > 0 {
		if *webhookURL != "" {
			if err := watcher.PostWebhook(*webhookURL, newFindings); err != nil {
				fmt.Fprintln(os.Stderr, "warning: webhook post failed:", err)
			} else {
				fmt.Println("posted new findings to webhook")
			}
		} else {
			fmt.Println("no --webhook-url set; new findings were only logged above")
		}
	}

	if err := watcher.SaveState(*namespace, *stateName, watcher.KeysOf(current)); err != nil {
		fmt.Fprintln(os.Stderr, "error saving state:", err)
		return 2
	}
	return 0
}
