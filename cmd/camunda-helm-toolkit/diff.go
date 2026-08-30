package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/instdiff"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/report"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func runDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	releaseA := fs.String("release-a", "", "first installed release name (required)")
	namespaceA := fs.String("namespace-a", "default", "namespace of the first release")
	releaseB := fs.String("release-b", "", "second installed release name (required)")
	namespaceB := fs.String("namespace-b", "default", "namespace of the second release")
	format := fs.String("format", "text", "output format: text|json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *releaseA == "" || *releaseB == "" {
		fmt.Fprintln(os.Stderr, "error: --release-a and --release-b are both required (both releases are read from the same cluster context this tool is already pointed at)")
		return 2
	}

	rawA, err := live.GetHelmValues(*namespaceA, *releaseA)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: fetching", *releaseA, ":", err)
		return 2
	}
	rawB, err := live.GetHelmValues(*namespaceB, *releaseB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: fetching", *releaseB, ":", err)
		return 2
	}
	a, err := values.ParseYAML(rawA)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: parsing values for", *releaseA, ":", err)
		return 2
	}
	b, err := values.ParseYAML(rawB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: parsing values for", *releaseB, ":", err)
		return 2
	}

	changes := instdiff.Compare(map[string]interface{}(a), map[string]interface{}(b))

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		if err := report.WriteJSONValue(os.Stdout, changes); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	case "text", "":
		fmt.Print(instdiff.WriteText(changes, labelFor(*releaseA, *namespaceA), labelFor(*releaseB, *namespaceB)))
	default:
		fmt.Fprintf(os.Stderr, "error: unknown --format %q (want text|json)\n", *format)
		return 2
	}

	if len(changes) > 0 {
		return 1
	}
	return 0
}

func labelFor(release, namespace string) string {
	return fmt.Sprintf("%s (%s)", release, namespace)
}
