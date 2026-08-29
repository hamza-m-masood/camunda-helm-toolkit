package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/live"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/report"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/upgrade"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
	"gopkg.in/yaml.v3"
)

func runUpgrade(args []string) int {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	release := fs.String("release", "", "installed Helm release name")
	namespace := fs.String("namespace", "default", "kubernetes namespace")
	fs.StringVar(namespace, "n", "default", "kubernetes namespace (shorthand)")
	valuesFile := fs.String("values-file", "", "read values from a file instead of a cluster")
	fs.StringVar(valuesFile, "f", "", "read values from a file instead of a cluster (shorthand)")
	from := fs.String("from", "", "current Camunda line, e.g. 8.9 (required with --values-file)")
	to := fs.String("to", "", "target Camunda line, e.g. 8.10 (default: newest with embedded data)")
	writeValues := fs.String("write-values", "", "write the migrated values.yaml to this path")
	stripRemoved := fs.Bool("strip-removed", false, "also delete removed keys from the migrated values")
	jsonOut := fs.Bool("json", false, "emit findings as JSON")
	noColor := fs.Bool("no-color", false, "disable color")
	failOn := fs.String("fail-on", "high", "minimum severity for nonzero exit: critical|high|medium|low")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := upgrade.LoadStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	req, err := buildRequest(store, *release, *namespace, *valuesFile, *from, *to, *stripRemoved)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	plan, err := req.Plan(store)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	if *writeValues != "" {
		if err := writeMigratedValues(*writeValues, plan.MigratedValues); err != nil {
			fmt.Fprintln(os.Stderr, "error writing migrated values:", err)
			return 2
		}
	}

	if *jsonOut {
		if err := writeUpgradeJSON(os.Stdout, plan); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	} else {
		writeUpgradeText(os.Stdout, plan, !*noColor, *writeValues)
	}
	return exitCode(plan.Findings, *failOn)
}

// buildRequest resolves where the operator is starting from. Reading it off the cluster
// is the whole point of the command: someone who does not know the chart well is also
// the person least able to tell you which chart version they are on.
func buildRequest(store *upgrade.Store, release, namespace, valuesFile, from, to string, strip bool) (upgrade.Request, error) {
	req := upgrade.Request{Release: release, StripRemoved: strip}
	// Namespace only means something when there is a release to read; leaving it unset
	// for a values-file run keeps the runbook from printing a misleading "-n default".
	if release != "" {
		req.Namespace = namespace
	}

	target := store.Latest()
	if to != "" {
		parsed, err := upgrade.ParseLine(to)
		if err != nil {
			return req, err
		}
		target = parsed
	}
	req.To = target

	switch {
	case valuesFile != "":
		if from == "" {
			return req, fmt.Errorf("--from is required with --values-file (a values file does not record its chart version)")
		}
		parsed, err := upgrade.ParseLine(from)
		if err != nil {
			return req, err
		}
		req.From = parsed
		raw, err := os.ReadFile(valuesFile)
		if err != nil {
			return req, err
		}
		v, err := values.ParseYAML(raw)
		if err != nil {
			return req, err
		}
		req.UserValues = v
		return req, nil

	case release != "":
		raw, err := live.GetHelmUserValues(namespace, release)
		if err != nil {
			return req, err
		}
		v, err := values.ParseYAML(raw)
		if err != nil {
			return req, err
		}
		req.UserValues = v

		if from != "" {
			parsed, err := upgrade.ParseLine(from)
			if err != nil {
				return req, err
			}
			req.From = parsed
			return req, nil
		}
		md, err := live.GetReleaseMetadata(namespace, release)
		if err != nil {
			return req, fmt.Errorf("%w (pass --from to skip this lookup)", err)
		}
		req.ChartVersion = md.Version
		line, ok := upgrade.LineFromChartVersion(md.Version)
		if !ok {
			return req, fmt.Errorf("cannot map chart version %q to a Camunda line; pass --from explicitly", md.Version)
		}
		req.From = line
		return req, nil

	default:
		return req, fmt.Errorf("one of --release or --values-file is required")
	}
}

func writeMigratedValues(path string, v values.Values) error {
	data, err := yaml.Marshal(map[string]interface{}(v))
	if err != nil {
		return err
	}
	header := "# Migrated by camunda-chart-doctor upgrade. Review before use.\n" +
		"# Renamed keys have been rewritten. Anything the report lists as removed but\n" +
		"# still present here needs a decision from you.\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}

type upgradeJSON struct {
	*upgrade.Plan
	MigratedValues map[string]interface{} `json:"migratedValues,omitempty"`
}

func writeUpgradeJSON(w io.Writer, p *upgrade.Plan) error {
	report.Sort(p.Findings)
	if p.Findings == nil {
		p.Findings = []rules.Finding{}
	}
	return report.WriteJSONValue(w, upgradeJSON{Plan: p, MigratedValues: p.MigratedValues})
}

func writeUpgradeText(w io.Writer, p *upgrade.Plan, useColor bool, wroteValues string) {
	fmt.Fprintf(w, "Upgrade plan: %s -> %s", p.From, p.To)
	if p.TargetChartVersion != "" {
		fmt.Fprintf(w, " (chart %s)", p.TargetChartVersion)
	}
	fmt.Fprintln(w)
	if p.Release != "" {
		fmt.Fprintf(w, "Release: %s in namespace %s", p.Release, p.Namespace)
		if p.ChartVersion != "" {
			fmt.Fprintf(w, ", currently on chart %s", p.ChartVersion)
		}
		fmt.Fprintln(w)
	}
	if len(p.Hops) == 0 {
		fmt.Fprintf(w, "\nAlready on %s. Nothing to plan.\n", p.To)
		return
	}
	fmt.Fprintf(w, "Hops: %s -> %s\n", p.From, strings.Join(p.Hops, " -> "))
	if quiet := hopsWithoutSteps(p); len(quiet) > 0 {
		fmt.Fprintf(w, "No imperative steps are known for: %s (values-key changes only).\n",
			strings.Join(quiet, ", "))
	}
	fmt.Fprintln(w)

	report.WriteText(w, p.Findings, useColor)

	if p.Approximate > 0 {
		fmt.Fprintf(w, "%d finding(s) are approximate: the chart's own condition was too complex to\n"+
			"evaluate exactly, so they are reported on key presence alone.\n\n", p.Approximate)
	}

	if len(p.ValueChanges) > 0 {
		fmt.Fprintf(w, "Values rewritten (%d):\n", len(p.ValueChanges))
		for _, c := range p.ValueChanges {
			if c.To != "" {
				fmt.Fprintf(w, "  [%s] %s  ->  %s\n", c.Hop, c.From, c.To)
				continue
			}
			fmt.Fprintf(w, "  [%s] %s  (deleted)\n", c.Hop, c.From)
		}
		fmt.Fprintln(w)
	}
	if wroteValues != "" {
		fmt.Fprintf(w, "Migrated values written to %s\n\n", wroteValues)
	} else {
		fmt.Fprint(w, "Re-run with --write-values <path> to save the migrated values.yaml.\n\n")
	}

	writeRunbook(w, p)
}

func writeRunbook(w io.Writer, p *upgrade.Plan) {
	if len(p.Runbook) == 0 {
		return
	}
	fmt.Fprintln(w, "Runbook")
	fmt.Fprintln(w, "=======")
	fmt.Fprintln(w, "Nothing below is executed for you. Read each step before running it.")
	fmt.Fprintln(w)
	n := 0
	for _, s := range p.Runbook {
		if s.Kind == upgrade.StepContingency {
			fmt.Fprintf(w, "IF NEEDED  %s\n", s.Title)
		} else {
			n++
			label := string(s.Kind)
			if s.Danger != upgrade.DangerSafe {
				label += ", " + string(s.Danger)
			}
			fmt.Fprintf(w, "%d. %s  [%s]\n", n, s.Title, label)
		}
		for _, line := range strings.Split(s.Why, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Fprintf(w, "   %s\n", strings.TrimSpace(line))
		}
		for _, c := range s.Commands {
			fmt.Fprintf(w, "   $ %s\n", c)
		}
		if s.Docs != "" {
			fmt.Fprintf(w, "   docs: %s\n", s.Docs)
		}
		if s.Source != "" {
			fmt.Fprintf(w, "   source: %s\n", s.Source)
		}
		fmt.Fprintln(w)
	}
}

func hopsWithoutSteps(p *upgrade.Plan) []string {
	var out []string
	for _, h := range p.Hops {
		if !p.HopsWithSteps[h] {
			out = append(out, h)
		}
	}
	return out
}
