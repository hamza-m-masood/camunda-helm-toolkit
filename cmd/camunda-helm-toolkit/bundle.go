package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/bundle"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func runBundle(args []string) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	release := fs.String("release", "", "installed release name (required)")
	namespace := fs.String("namespace", "default", "kubernetes namespace")
	fs.StringVar(namespace, "n", "default", "kubernetes namespace (shorthand)")
	out := fs.String("o", "", "output tar.gz path (required unless --dry-run)")
	dryRun := fs.Bool("dry-run", false, "list what would be collected, without collecting anything")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *release == "" {
		fmt.Fprintln(os.Stderr, "error: --release is required")
		return 2
	}

	if *dryRun {
		fmt.Println("This would collect the following into the bundle (nothing is touched in --dry-run):")
		for _, name := range bundle.Plan(*release, *namespace) {
			fmt.Println("  " + name)
		}
		fmt.Println("\nvalues-redacted.yaml has every password/secret/token/credential/*Key value")
		fmt.Println("replaced with <redacted> before it is ever written to disk. describe/events")
		fmt.Println("output has node names and IP addresses redacted the same way.")
		return 0
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "error: -o <path.tar.gz> is required (or pass --dry-run)")
		return 2
	}

	findings, err := runLiveFindings(*release, *namespace)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not compute findings:", err)
		findings = nil
	}
	findingsJSON, _ := json.MarshalIndent(findings, "", "  ")

	files, err := bundle.Collect(*release, *namespace, findingsJSON)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	if err := bundle.WriteArchive(*out, files); err != nil {
		fmt.Fprintln(os.Stderr, "error writing archive:", err)
		return 2
	}

	fmt.Printf("Wrote %s (%d files). Contents are listed in manifest.json inside the archive —\n", *out, len(files))
	fmt.Println("review it before sending. values-redacted.yaml has credential-shaped values redacted;")
	fmt.Println("this is best-effort, not a guarantee — skim it yourself before attaching to a ticket.")
	return 0
}

// runLiveFindings mirrors the --release --live path of `check`, without the
// suppression/formatting layer — a support bundle should show everything found,
// unfiltered, for a support engineer to triage.
func runLiveFindings(release, namespace string) ([]rules.Finding, error) {
	raw, err := live.GetHelmValues(namespace, release)
	if err != nil {
		return nil, err
	}
	effective, err := values.ParseYAML(raw)
	if err != nil {
		return nil, err
	}
	var findings []rules.Finding
	for _, check := range rules.AllValuesChecks() {
		findings = append(findings, check(effective)...)
	}
	findings = append(findings, live.CheckPDBObjects(namespace, release)...)
	for _, b := range values.FindBlocks(effective, "existingSecret") {
		es, _ := b.Node["existingSecret"].(string)
		esk, _ := b.Node["existingSecretKey"].(string)
		if es == "" || esk == "" {
			continue
		}
		findings = append(findings, live.CheckSecretKeyExists(namespace, es, esk, b.Path)...)
	}
	return findings, nil
}
