// Command camunda-helm-toolkit is a pre-flight, live, upgrade, and bootstrap checker for
// Camunda 8 Self-Managed Helm installs. It is an unofficial, community-maintained tool —
// see README.md for what it does and does not guarantee.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/customrules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/helmrender"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/policyeval"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/report"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/suppress"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
	"gopkg.in/yaml.v3"
)

const defaultIgnoreFile = ".chartdoctor-ignore.yaml"

// version is set at release build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	report.ToolVersion = version
	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "diff":
		os.Exit(runDiff(os.Args[2:]))
	case "upgrade":
		os.Exit(runUpgrade(os.Args[2:]))
	case "generate":
		os.Exit(runGenerate(os.Args[2:]))
	case "plan-secrets":
		os.Exit(runPlanSecrets(os.Args[2:]))
	case "bundle":
		os.Exit(runBundle(os.Args[2:]))
	case "scaffold-monitoring":
		os.Exit(runScaffoldMonitoring(os.Args[2:]))
	case "size":
		os.Exit(runSize(os.Args[2:]))
	case "watch-once":
		os.Exit(runWatchOnce(os.Args[2:]))
	case "scaffold-watcher":
		os.Exit(runScaffoldWatcher(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("camunda-helm-toolkit " + version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`camunda-helm-toolkit — pre-flight, live, upgrade, and bootstrap checks for Camunda 8 Self-Managed Helm installs

USAGE:
  camunda-helm-toolkit init --chart <path>                            Build a self-checking starter values.yaml
  camunda-helm-toolkit check --chart <path> [-f values.yaml]...       Pre-install: chart + overlay(s)
  camunda-helm-toolkit check --release <name> -n <namespace>          Post-install: an installed release
                              [--chart <path>] [--live]
  camunda-helm-toolkit diff --release-a <n1> --release-b <n2>         Structural diff between two installed releases
  camunda-helm-toolkit upgrade --release <name> -n <namespace>        Plan an upgrade to a newer chart line
                              [--to 8.10] [--write-values out.yaml]
  camunda-helm-toolkit upgrade -f values.yaml --from 8.9 [--to 8.10]  Plan from a values file, no cluster
  camunda-helm-toolkit generate --chart-repo <path>                   Regenerate embedded upgrade migration data
  camunda-helm-toolkit plan-secrets --release <name> -n <namespace>   Pin chart-managed secrets before an upgrade
  camunda-helm-toolkit bundle --release <name> -n <namespace> -o f.tgz Collect a redacted support bundle
  camunda-helm-toolkit scaffold-monitoring --release <name> --chart <path> Generate ServiceMonitor + PrometheusRule
  camunda-helm-toolkit size --throughput <cmds/sec> --avg-payload-kb <n> Heuristic clusterSize/pvcSize starting point
  camunda-helm-toolkit scaffold-watcher --release <name> --schedule "<cron>" Generate a continuous drift-check CronJob
  camunda-helm-toolkit watch-once --release <name> -n <namespace>     Run one drift check (used by the CronJob)
  camunda-helm-toolkit version

Run any command with --help for its full flag list. See README.md for what each one does
and how it was validated.

FLAGS (init):
  --chart string           Path to the chart to build a starter values.yaml for (required)
  --non-interactive        Fail on any missing answer instead of prompting (for CI/scripting)
  --write-values string    Write the generated values.yaml here instead of stdout
  --release-name string    What you intend to "helm install <name>" as (default "camunda")
  --enable string          Component to enable beyond orchestration (repeatable): identity, connectors, optimize, webModeler
  --secondary-storage string  elasticsearch, opensearch, or rdbms (required)
  --throughput float       Sustained commands/sec, for a sizing recommendation (optional)
  --avg-payload-kb float   Average command payload size in KB (required if --throughput is set)
  --retention-days int     How long exported records must stay queryable (optional)
  --ingress-host string    Expose via ingress at this host (optional)
  --ingress-tls            Enable TLS on the ingress
  --auth-method string     basic or oidc (required)
  --admin-username string  Initial admin username (required if --auth-method basic)
  --admin-password string  Initial admin password (required if --auth-method basic)
  --oidc-issuer-url string External OIDC issuer URL (required if --auth-method oidc)
  --web-modeler-from-email string  Web Modeler notification sender address (required if --enable webModeler)

FLAGS (check):
  --chart string          Path to the camunda-platform Helm chart (enables manifest-based checks)
  -f, --values string      Values overlay file (repeatable, applied in order — Helm merge semantics)
  --release string         Installed Helm release name (uses "helm get values -a" instead of --chart+-f)
  -n, --namespace string   Kubernetes namespace of the release (default "default")
  --live                   Also query the live cluster (kubectl/oc) for drift values alone can't show
  --json                   Emit findings as JSON instead of text (shorthand for --format json)
  --format string          Output format: text|json|sarif (default "text"; --json overrides to "json")
  --no-color               Disable ANSI color in text output
  --fail-on string         Minimum severity causing a nonzero exit: critical|high|medium|low (default "high")
  --ignore-file string     Suppression file (default: auto-load .chartdoctor-ignore.yaml from cwd if present)
  --show-suppressed        Also print/emit suppressed findings, not just their count
  --rules-from string      Custom rules file, local path or http(s):// URL (repeatable) — see README "Custom rules"
  --policy-dir string      Directory of Conftest/Rego policies to run against the rendered manifests (requires
                            --chart and conftest on PATH) — see README "Policy layer (Conftest/Rego)"

FLAGS (diff):
  --release-a string       First installed release name (required)
  --namespace-a string     Namespace of the first release (default "default")
  --release-b string       Second installed release name (required)
  --namespace-b string     Namespace of the second release (default "default")
  --format string          Output format: text|json (default "text")

Both releases are read from the same cluster context this tool is already pointed at —
comparing across two different kubeconfig contexts/clusters isn't supported.

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
	jsonOut := fs.Bool("json", false, "JSON output (shorthand for --format json)")
	format := fs.String("format", "text", "output format: text|json|sarif")
	noColor := fs.Bool("no-color", false, "disable color")
	failOn := fs.String("fail-on", "high", "minimum severity for nonzero exit")
	ignoreFile := fs.String("ignore-file", "", "suppression file (default: auto-load "+defaultIgnoreFile+" from cwd)")
	showSuppressed := fs.Bool("show-suppressed", false, "also print/emit suppressed findings")
	var rulesFrom multiFlag
	fs.Var(&rulesFrom, "rules-from", "custom rules file, local path or http(s):// URL (repeatable)")
	policyDir := fs.String("policy-dir", "", "directory of Conftest/Rego policies to evaluate against the rendered manifests (requires --chart and conftest on PATH)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// A flag-combination error like this one depends only on the flags themselves,
	// not on anything fetched from a chart or cluster — check it before doing any of
	// that work, both so the failure is instant and so a --release that requires a
	// real cluster to resolve doesn't have to succeed first just to reach this check.
	if *policyDir != "" && *chart == "" {
		fmt.Fprintln(os.Stderr, "error: --policy-dir requires --chart (Conftest evaluates the rendered manifests, which need a chart to render)")
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

	for _, src := range rulesFrom {
		rf, err := customrules.LoadFrom(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: loading --rules-from", src, ":", err)
			return 2
		}
		findings = append(findings, rf.Evaluate(effective)...)
	}

	if *chart != "" {
		// A real release name matters here too if it's known — a future manifest check
		// could reasonably key off labels, so don't hand it a placeholder just because
		// today's checks happen not to care.
		renderRelease := *release
		if renderRelease == "" {
			renderRelease = noReleaseNamePlaceholder
		}
		docs, err := renderManifests(*chart, effective, renderRelease)
		if err != nil {
			if *policyDir != "" {
				// Built-in manifest checks degrade gracefully on a render failure
				// (a soft warning further down covers that), but --policy-dir was
				// explicitly requested — silently skipping it here would be the
				// same "looks configured, isn't" gap the conftest-on-PATH check
				// above exists to prevent, just triggered a different way.
				fmt.Fprintln(os.Stderr, "error: --policy-dir requires the chart to render, and it didn't:", err)
				return 2
			}
			fmt.Fprintln(os.Stderr, "warning: skipping manifest checks:", err)
		} else {
			for _, check := range rules.AllManifestChecks() {
				findings = append(findings, check(docs)...)
			}
			if *policyDir != "" {
				pf, err := policyeval.Run(*policyDir, docs)
				if err != nil {
					fmt.Fprintln(os.Stderr, "error: --policy-dir:", err)
					return 2
				}
				findings = append(findings, pf...)
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

	kept, suppressed, err := applySuppressions(findings, *ignoreFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	outFormat := strings.ToLower(strings.TrimSpace(*format))
	if *jsonOut {
		outFormat = "json"
	}
	switch outFormat {
	case "sarif":
		// SARIF results are meant to be exactly what's actionable; suppressed
		// findings are intentionally left out of the results array (see
		// report.WriteSARIF's doc comment) rather than force-fit into a SARIF
		// suppression object this alpha tool doesn't try to model correctly.
		if err := report.WriteSARIF(os.Stdout, kept); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	case "json":
		if err := report.WriteJSON(os.Stdout, kept, suppressed, *showSuppressed); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	case "text", "":
		report.WriteText(os.Stdout, kept, suppressed, *showSuppressed, !*noColor)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown --format %q (want text|json|sarif)\n", *format)
		return 2
	}

	return exitCode(kept, *failOn)
}

// applySuppressions loads a suppression file (explicit path, or an auto-detected
// .chartdoctor-ignore.yaml in cwd) and splits findings into kept/suppressed. An
// explicitly-named file that fails to load is an error; a missing auto-detected file
// just means "no suppressions configured".
func applySuppressions(findings []rules.Finding, ignoreFile string) (kept, suppressed []rules.Finding, err error) {
	path := ignoreFile
	explicit := path != ""
	if path == "" {
		path = defaultIgnoreFile
	}
	f, loadErr := suppress.Load(path)
	if loadErr != nil {
		if explicit || !os.IsNotExist(loadErr) {
			return nil, nil, loadErr
		}
		return findings, nil, nil
	}
	kept, suppressed = f.Apply(findings)
	return kept, suppressed, nil
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

// noReleaseNamePlaceholder is used only when no real release name is available (e.g.
// `check --chart` in pre-install mode, before anything is ever `helm install`ed). It
// must never be used by a caller that generates release-scoped resources — see
// helmrender.Template's doc comment.
const noReleaseNamePlaceholder = "camunda-helm-toolkit"

// renderManifests writes the effective (already-merged) values to a temp file and
// templates the chart against them, so manifest-based checks see exactly the same
// configuration the values-based checks just evaluated. releaseName should be the real
// installed (or intended) release name whenever one is known; pass
// noReleaseNamePlaceholder only when there genuinely isn't one yet.
func renderManifests(chart string, effective values.Values, releaseName string) ([]rules.ManifestDoc, error) {
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
	raw, err := helmrender.Template(chart, []string{tmp.Name()}, releaseName)
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
