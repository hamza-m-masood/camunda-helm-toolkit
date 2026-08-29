// Package helmrender shells out to the `helm` binary to obtain a chart's default values
// and its fully rendered manifests. Shelling out (rather than vendoring Helm's Go
// packages) keeps this tool's dependency footprint small and its behavior identical to
// whatever `helm` version the operator already has installed.
package helmrender

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

// ShowValues returns the chart's own default values.yaml content.
func ShowValues(chartPath string) ([]byte, error) {
	cmd := exec.Command("helm", "show", "values", chartPath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm show values failed: %w: %s", err, stderr.String())
	}
	return out.Bytes(), nil
}

// Template renders the chart with the given value overlay files and returns the raw
// multi-document YAML manifest output.
func Template(chartPath string, valueFiles []string) ([]byte, error) {
	args := []string{"template", "camunda-helm-toolkit", chartPath}
	for _, f := range valueFiles {
		args = append(args, "-f", f)
	}
	cmd := exec.Command("helm", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template failed: %w: %s", err, stderr.String())
	}
	return out.Bytes(), nil
}

var (
	kindRe = regexp.MustCompile(`(?m)^kind:\s*(\S+)`)
	nameRe = regexp.MustCompile(`(?m)^\s*name:\s*(\S+)`)
)

// SplitDocs splits multi-document YAML into rules.ManifestDoc values, extracting
// kind/name with a lightweight regex pass rather than a full YAML parse of every
// document — sufficient for the pattern-matching checks that consume it.
func SplitDocs(raw []byte) []rules.ManifestDoc {
	parts := strings.Split(string(raw), "\n---\n")
	var docs []rules.ManifestDoc
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kind := "Unknown"
		if m := kindRe.FindStringSubmatch(p); m != nil {
			kind = m[1]
		}
		name := "unknown"
		if m := nameRe.FindStringSubmatch(p); m != nil {
			name = m[1]
		}
		docs = append(docs, rules.ManifestDoc{Kind: kind, Name: name, Text: p})
	}
	return docs
}
