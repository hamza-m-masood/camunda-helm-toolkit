// Command camunda-chart-doctor is a pre-flight, live, and upgrade checker for Camunda 8
// Self-Managed Helm installs. It is an unofficial, community-maintained tool — see
// README.md for what it does and does not guarantee.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/helmrender"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/live"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/report"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
	"gopkg.in/yaml.v3"
)

// version is set at release build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "upgrade":
		os.Exit(runUpgrade(os.Args[2:]))
	case "generate":
		os.Exit(runGenerate(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("camunda-chart-doctor " + version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`camunda-chart-doctor — pre-flight, live, and upgrade checks for Camunda 8 Self-Managed Helm installs

USAGE:
  camunda-chart-doctor check --chart <path> [-f values.yaml]...       Pre-install: chart + overlay(s)
  camunda-chart-doctor check --release <name> -n <namespace>          Post-install: an installed release
                              [--chart <path>] [--live]
  camunda-chart-doctor upgrade --release <name> -n <namespace>        Plan an upgrade to a newer chart line
                              [--to 8.10] [--write-values out.yaml]
  camunda-chart-doctor upgrade -f values.yaml --from 8.9 [--to 8.10]  Plan from a values file, no cluster
  camunda-chart-doctor version

FLAGS (check):
  --chart string          Path to the camunda-platform Helm chart (enables manifest-based checks)
  -f, --values string      Values overlay file (repeatable, applied in order — Helm merge semantics)
  --release string         Installed Helm release name (uses "helm get values -a" instead of --chart+-f)
  -n, --namespace string   Kubernetes namespace of the release (default "default")
  --live                   Also query the live cluster (kubectl/oc) for drift values alone can't show
  --json                   Emit findings as JSON instead of text
  --no-color               Disable ANSI color in text output
  --fail-on string         Minimum severity causing a nonzero exit: critical|high|medium|low (default "high")

FLAGS (upgrade):
  --release string         Installed Helm release name (reads the values you supplied, not chart defaults)
  -n, --namespace string   Kubernetes namespace of the release (default "default")
  -f, --values-file string Plan from a values file instead of a cluster (requires --from)
  --from string            Current Camunda line, e.g. 8.9 (auto-detected from the release otherwise)
  --to string              Target Camunda line, e.g. 8.10 (default: newest line this build knows)
  --write-values string    Write the migrated values.yaml to this path
  --strip-removed          Also delete removed keys from the migrated values (renames always applied)
  --json                   Emit the full plan as JSON
  --fail-on string         Minimum severity causing a nonzero exit (default "high")

The upgrade command never writes to your cluster. Its runbook prints commands for you
to run yourself, labelled safe / destructive / downtime.

This is an ALPHA, community-maintained tool. It is not an official Camunda product and carries
no support guarantee. It encodes a known set of recurring misconfigurations — passing every
check is not proof a deployment is production-ready. See README.md.`)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	chart := fs.String("chart", "", "path to chart")
	var valueFiles multiFlag
	fs.Var(&valueFiles, "f", "values overlay file (repeatable)")
	fs.Var(&valueFiles, "values", "values overlay file (repeatable)")
	release := fs.String("release", "", "installed release name")
	namespace := fs.String("namespace", "default", "kubernetes namespace")
	fs.StringVar(namespace, "n", "default", "kubernetes namespace (shorthand)")
	liveFlag := fs.Bool("live", false, "also run live cluster checks")
	jsonOut := fs.Bool("json", false, "JSON output")
	noColor := fs.Bool("no-color", false, "disable color")
	failOn := fs.String("fail-on", "high", "minimum severity for nonzero exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	effective, err := resolveEffectiveValues(*chart, *release, *namespace, valueFiles)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	var findings []rules.Finding
	for _, check := range rules.AllValuesChecks() {
		findings = append(findings, check(effective)...)
	}

	if *chart != "" {
		if docs, err := renderManifests(*chart, effective); err != nil {
			fmt.Fprintln(os.Stderr, "warning: skipping manifest checks:", err)
		} else {
			for _, check := range rules.AllManifestChecks() {
				findings = append(findings, check(docs)...)
			}
		}
	}

	if *liveFlag {
		if *release == "" {
			fmt.Fprintln(os.Stderr, "warning: --live requires --release, skipping live checks")
		} else {
			findings = append(findings, live.CheckPDBObjects(*namespace, *release)...)
			for _, b := range values.FindBlocks(effective, "existingSecret") {
				es, _ := b.Node["existingSecret"].(string)
				esk, _ := b.Node["existingSecretKey"].(string)
				if es == "" || esk == "" {
					continue
				}
				findings = append(findings, live.CheckSecretKeyExists(*namespace, es, esk, b.Path)...)
			}
		}
	}

	if *jsonOut {
		if err := report.WriteJSON(os.Stdout, findings); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	} else {
		report.WriteText(os.Stdout, findings, !*noColor)
	}

	return exitCode(findings, *failOn)
}

func resolveEffectiveValues(chart, release, namespace string, overlays multiFlag) (values.Values, error) {
	if release != "" {
		raw, err := live.GetHelmValues(namespace, release)
		if err != nil {
			return nil, err
		}
		return values.ParseYAML(raw)
	}
	if chart == "" {
		return nil, fmt.Errorf("one of --chart or --release is required")
	}
	base, err := helmrender.ShowValues(chart)
	if err != nil {
		return nil, err
	}
	merged, err := values.ParseYAML(base)
	if err != nil {
		return nil, err
	}
	for _, f := range overlays {
		ov, err := loadYAMLFile(f)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", f, err)
		}
		merged = values.DeepMerge(merged, ov)
	}
	return merged, nil
}

func loadYAMLFile(path string) (values.Values, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return values.ParseYAML(data)
}

// renderManifests writes the effective (already-merged) values to a temp file and
// templates the chart against them, so manifest-based checks see exactly the same
// configuration the values-based checks just evaluated.
func renderManifests(chart string, effective values.Values) ([]rules.ManifestDoc, error) {
	tmp, err := os.CreateTemp("", "ccd-effective-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	data, err := yaml.Marshal(map[string]interface{}(effective))
	if err != nil {
		tmp.Close()
		return nil, err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	raw, err := helmrender.Template(chart, []string{tmp.Name()})
	if err != nil {
		return nil, err
	}
	return helmrender.SplitDocs(raw), nil
}

func exitCode(findings []rules.Finding, failOn string) int {
	threshold := severityFromString(failOn)
	worst := -1
	for _, f := range findings {
		r := rules.RankOf(f.Severity)
		if worst == -1 || r < worst {
			worst = r
		}
	}
	if worst == -1 || worst > threshold {
		return 0
	}
	switch worst {
	case 0:
		return 3
	case 1:
		return 2
	default:
		return 1
	}
}

func severityFromString(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 1
	}
}
