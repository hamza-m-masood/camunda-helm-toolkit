// Package policyeval shells out to Conftest to evaluate operator-authored Rego
// policies against a chart's rendered manifests — a separate, more expressive tier
// from internal/customrules' simple declarative YAML rules, for logic (cross-field
// conditions, anything Rego can express) that format can't reach. Conftest, not the
// OPA Go SDK, is shelled out to on purpose — matching how internal/helmrender shells
// to `helm` rather than vendoring it — to keep this project's own dependency footprint
// small regardless of how large a real policy engine's own dependency tree is.
package policyeval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

// conftestResult mirrors one entry of `conftest test -o json`'s output array —
// verified against a real conftest 1.x / OPA 1.x install, not assumed: a passing
// input carries only filename/namespace/successes, with failures/warnings present
// only when Conftest actually has something to report for that file.
type conftestResult struct {
	Filename string        `json:"filename"`
	Failures []conftestMsg `json:"failures,omitempty"`
	Warnings []conftestMsg `json:"warnings,omitempty"`
}

type conftestMsg struct {
	Msg string `json:"msg"`
}

// Run evaluates every .rego policy in policyDir against docs (the chart's rendered
// manifests) in a single `conftest test` invocation — one temp file per manifest
// document, all passed together, matched back to their ManifestDoc by filename.
// Requires `conftest` on PATH (built against Conftest's Rego v1 syntax — deny/warn
// rules need the `if`/`contains` keywords Conftest's current OPA runtime expects); if
// conftest is not found, this returns an error rather than silently skipping the
// policies — a --policy-dir the operator asked for that quietly evaluates nothing is
// exactly the "looks configured, isn't" failure class this tool exists to catch
// elsewhere, not repeat here.
func Run(policyDir string, docs []rules.ManifestDoc) ([]rules.Finding, error) {
	if _, err := exec.LookPath("conftest"); err != nil {
		return nil, fmt.Errorf("conftest not found on PATH — install it from https://www.conftest.dev/install/ to use --policy-dir")
	}
	if len(docs) == 0 {
		return nil, nil
	}

	byFilename := make(map[string]rules.ManifestDoc, len(docs))
	var files []string
	for i, d := range docs {
		tmp, err := os.CreateTemp("", fmt.Sprintf("ccd-policy-input-%03d-*.yaml", i))
		if err != nil {
			return nil, err
		}
		if _, err := tmp.WriteString(d.Text); err != nil {
			tmp.Close()
			return nil, err
		}
		if err := tmp.Close(); err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		files = append(files, tmp.Name())
		byFilename[tmp.Name()] = d
	}

	args := append([]string{"test", "--policy", policyDir, "-o", "json"}, files...)
	cmd := exec.Command("conftest", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var results []conftestResult
	// conftest exits nonzero the moment ANY input has a failure — that is the
	// expected "there are findings" case, not a tool-execution error. Only treat
	// this as a real error if the JSON itself doesn't parse (a genuine conftest
	// invocation or policy problem, e.g. a Rego syntax error).
	if jsonErr := json.Unmarshal(out.Bytes(), &results); jsonErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf("conftest test failed: %w: %s", runErr, stderr.String())
		}
		return nil, fmt.Errorf("parsing conftest output: %w (stdout: %s)", jsonErr, out.String())
	}

	var findings []rules.Finding
	for _, r := range results {
		doc, ok := byFilename[r.Filename]
		path := r.Filename
		if ok {
			path = doc.Kind + "/" + doc.Name
		}
		for _, f := range r.Failures {
			findings = append(findings, rules.Finding{
				RuleID:   "POLICY-DENY",
				Severity: rules.High,
				Title:    f.Msg,
				Path:     path,
			})
		}
		for _, w := range r.Warnings {
			findings = append(findings, rules.Finding{
				RuleID:   "POLICY-WARN",
				Severity: rules.Medium,
				Title:    w.Msg,
				Path:     path,
			})
		}
	}
	return findings, nil
}
