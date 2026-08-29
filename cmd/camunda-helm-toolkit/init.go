package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/helmrender"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/wizard"
	"gopkg.in/yaml.v3"
)

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	chart := fs.String("chart", "", "path to the chart to build a starter values.yaml for (required)")
	nonInteractive := fs.Bool("non-interactive", false, "fail on any missing answer instead of prompting (for CI/scripting)")
	writeValues := fs.String("write-values", "", "write the generated values.yaml here instead of stdout")

	releaseName := fs.String("release-name", "", "the name you intend to `helm install <name>` as (default: camunda)")
	var enable multiFlag
	fs.Var(&enable, "enable", "component to enable beyond orchestration (repeatable): identity, connectors, optimize, webModeler")
	secondaryStorage := fs.String("secondary-storage", "", "elasticsearch, opensearch, or rdbms (required -- the chart will not render without one)")
	throughput := fs.Float64("throughput", 0, "sustained commands/sec, for a sizing recommendation (optional -- 0 skips sizing)")
	avgPayloadKB := fs.Float64("avg-payload-kb", 0, "average command payload size in KB (required if --throughput is set)")
	retentionDays := fs.Int("retention-days", 0, "how long exported records must stay queryable (optional)")
	ingressHost := fs.String("ingress-host", "", "expose via ingress at this host (optional -- blank skips ingress entirely)")
	ingressTLS := fs.Bool("ingress-tls", false, "enable TLS on the ingress (only meaningful with --ingress-host)")
	authMethod := fs.String("auth-method", "", "basic or oidc (required)")
	adminUsername := fs.String("admin-username", "", "initial admin username (required if --auth-method basic)")
	adminPassword := fs.String("admin-password", "", "initial admin password (required if --auth-method basic)")
	oidcIssuerURL := fs.String("oidc-issuer-url", "", "external OIDC issuer URL (required if --auth-method oidc)")
	webModelerFromEmail := fs.String("web-modeler-from-email", "", "sender address for Web Modeler notification emails (required if --enable webModeler)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *chart == "" {
		fmt.Fprintln(os.Stderr, "error: --chart is required")
		return 2
	}

	in := bufio.NewScanner(os.Stdin)
	ask := func(flagName, label, def string, required bool) (string, error) {
		if *nonInteractive {
			if required {
				return "", fmt.Errorf("--%s is required with --non-interactive", flagName)
			}
			return def, nil
		}
		return promptText(in, label, def, required)
	}

	a := wizard.Answers{
		ReleaseName:          *releaseName,
		SecondaryStorageType: *secondaryStorage,
		ThroughputPerSec:     *throughput,
		AvgPayloadKB:         *avgPayloadKB,
		RetentionDays:        *retentionDays,
		IngressHost:          *ingressHost,
		IngressTLS:           *ingressTLS,
		AuthMethod:           *authMethod,
		AdminUsername:        *adminUsername,
		AdminPassword:        *adminPassword,
		OIDCIssuerURL:        *oidcIssuerURL,
		WebModelerFromEmail:  *webModelerFromEmail,
	}

	var err error
	if a.ReleaseName == "" {
		if a.ReleaseName, err = ask("release-name", "Release name (what you'll `helm install <name>`)", "camunda", false); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		if a.ReleaseName == "" {
			a.ReleaseName = "camunda"
		}
	}

	if a.SecondaryStorageType == "" {
		if a.SecondaryStorageType, err = ask("secondary-storage", "Secondary storage (elasticsearch/opensearch/rdbms)", "", true); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}

	enabledSet := map[string]bool{}
	for _, c := range enable {
		enabledSet[strings.ToLower(strings.TrimSpace(c))] = true
	}
	if len(enable) > 0 {
		a.EnableIdentity = enabledSet["identity"]
		a.EnableConnectors = enabledSet["connectors"]
		a.EnableOptimize = enabledSet["optimize"]
		a.EnableWebModeler = enabledSet["webmodeler"]
	} else if !*nonInteractive {
		a.EnableIdentity = promptBool(in, "Enable Identity?", false)
		a.EnableConnectors = promptBool(in, "Enable Connectors?", false)
		a.EnableOptimize = promptBool(in, "Enable Optimize?", false)
		a.EnableWebModeler = promptBool(in, "Enable Web Modeler?", false)
	}
	if a.EnableWebModeler && a.WebModelerFromEmail == "" {
		if a.WebModelerFromEmail, err = ask("web-modeler-from-email", "Web Modeler notification sender address", "", true); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}

	if a.AuthMethod == "" {
		if a.AuthMethod, err = ask("auth-method", "Auth method (basic/oidc)", "basic", true); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}
	switch a.AuthMethod {
	case "basic":
		if a.AdminUsername == "" {
			if a.AdminUsername, err = ask("admin-username", "Initial admin username", "", true); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 2
			}
		}
		if a.AdminPassword == "" {
			if a.AdminPassword, err = ask("admin-password", "Initial admin password", "", true); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 2
			}
		}
	case "oidc":
		if a.OIDCIssuerURL == "" {
			if a.OIDCIssuerURL, err = ask("oidc-issuer-url", "External OIDC issuer URL", "", true); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 2
			}
		}
	}

	if a.ThroughputPerSec == 0 && !*nonInteractive {
		if promptBool(in, "Add a sizing recommendation (throughput/payload)?", false) {
			a.ThroughputPerSec = promptFloat(in, "Sustained commands/sec", 0)
			a.AvgPayloadKB = promptFloat(in, "Average payload size (KB)", 2)
		}
	}
	if a.ThroughputPerSec > 0 && a.AvgPayloadKB <= 0 {
		fmt.Fprintln(os.Stderr, "error: --avg-payload-kb is required when --throughput is set")
		return 2
	}

	if a.IngressHost == "" && !*nonInteractive {
		a.IngressHost, _ = promptText(in, "Ingress host (blank to skip)", "", false)
		if a.IngressHost != "" {
			a.IngressTLS = promptBool(in, "Enable TLS on the ingress?", true)
		}
	}

	base, err := helmrender.ShowValues(*chart)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	chartDefaults, err := values.ParseYAML(base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	overlay, notes, err := wizard.Build(a, chartDefaults)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	merged := values.DeepMerge(chartDefaults, overlay)

	var findings []rules.Finding
	for _, check := range rules.AllValuesChecks() {
		findings = append(findings, check(merged)...)
	}
	// A render failure means the generated values.yaml cannot actually be installed at
	// all -- strictly worse than any individual check finding, and never something to
	// paper over as "skipping manifest rules" while still reporting success below.
	docs, rmErr := renderManifests(*chart, merged)
	if rmErr != nil {
		fmt.Fprintln(os.Stderr, "error: the generated values.yaml does not render with this chart -- this is a bug in `init`, not something to use:")
		fmt.Fprintln(os.Stderr, " ", rmErr)
		return 1
	}
	for _, check := range rules.AllManifestChecks() {
		findings = append(findings, check(docs)...)
	}

	var unexpected []rules.Finding
	for _, f := range findings {
		if wizard.IsDocumentedException(f) {
			continue
		}
		unexpected = append(unexpected, f)
	}
	if len(unexpected) > 0 {
		fmt.Fprintln(os.Stderr, "internal error: the generated values.yaml did not pass its own self-check -- this is a bug in `init`, not something to use. Please report it. Findings:")
		for _, f := range unexpected {
			fmt.Fprintf(os.Stderr, "  [%s] %s at %s\n", f.RuleID, f.Title, f.Path)
		}
		return 1
	}

	out, err := yaml.Marshal(map[string]interface{}(overlay))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	if *writeValues != "" {
		if err := os.WriteFile(*writeValues, out, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error writing", *writeValues, ":", err)
			return 2
		}
		fmt.Fprintln(os.Stderr, "wrote", *writeValues)
	} else {
		os.Stdout.Write(out)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "This passed its own self-check (every `check` rule this tool can close via")
	fmt.Fprintln(os.Stderr, "values.yaml alone), but that is not proof it is production-ready -- see the")
	fmt.Fprintln(os.Stderr, "notes below and README.md.")
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "- "+n)
	}
	return 0
}

func promptText(in *bufio.Scanner, label, def string, required bool) (string, error) {
	suffix := ""
	if def != "" {
		suffix = fmt.Sprintf(" [%s]", def)
	}
	for {
		fmt.Fprintf(os.Stderr, "%s%s: ", label, suffix)
		if !in.Scan() {
			return "", fmt.Errorf("unexpected end of input reading %q", label)
		}
		v := strings.TrimSpace(in.Text())
		if v == "" {
			v = def
		}
		if v == "" && required {
			fmt.Fprintln(os.Stderr, "  a value is required")
			continue
		}
		return v, nil
	}
}

func promptBool(in *bufio.Scanner, label string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	fmt.Fprintf(os.Stderr, "%s [%s]: ", label, d)
	if !in.Scan() {
		return def
	}
	v := strings.ToLower(strings.TrimSpace(in.Text()))
	switch v {
	case "":
		return def
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return def
	}
}

func promptFloat(in *bufio.Scanner, label string, def float64) float64 {
	fmt.Fprintf(os.Stderr, "%s [%v]: ", label, def)
	if !in.Scan() {
		return def
	}
	v := strings.TrimSpace(in.Text())
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
