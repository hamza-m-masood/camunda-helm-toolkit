package policyeval_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/policyeval"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

func requireConftest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("conftest"); err != nil {
		t.Skip("conftest not on PATH — skipping real Conftest integration test")
	}
}

const denyAndWarnPolicy = `package main

deny contains msg if {
	input.kind == "ConfigMap"
	msg := "test deny message"
}

warn contains msg if {
	input.kind == "ConfigMap"
	msg := "test warn message"
}
`

func TestRun_DenyAndWarnMapToFindings(t *testing.T) {
	requireConftest(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.rego"), []byte(denyAndWarnPolicy), 0o644); err != nil {
		t.Fatal(err)
	}

	docs := []rules.ManifestDoc{
		{Kind: "ConfigMap", Name: "flagged", Text: "kind: ConfigMap\nmetadata:\n  name: flagged\n"},
		{Kind: "Secret", Name: "clean", Text: "kind: Secret\nmetadata:\n  name: clean\n"},
	}

	findings, err := policyeval.Run(dir, docs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var denyCount, warnCount int
	for _, f := range findings {
		switch f.RuleID {
		case "POLICY-DENY":
			denyCount++
			if f.Severity != rules.High {
				t.Errorf("expected POLICY-DENY to be High severity, got %s", f.Severity)
			}
			if f.Path != "ConfigMap/flagged" {
				t.Errorf("expected finding path ConfigMap/flagged, got %s", f.Path)
			}
		case "POLICY-WARN":
			warnCount++
			if f.Severity != rules.Medium {
				t.Errorf("expected POLICY-WARN to be Medium severity, got %s", f.Severity)
			}
		default:
			t.Errorf("unexpected RuleID %s", f.RuleID)
		}
	}
	if denyCount != 1 {
		t.Errorf("expected exactly 1 POLICY-DENY finding, got %d", denyCount)
	}
	if warnCount != 1 {
		t.Errorf("expected exactly 1 POLICY-WARN finding, got %d", warnCount)
	}
	// The Secret document matches neither rule body (input.kind == "ConfigMap" is
	// false for it) so it must contribute zero findings — proves this isn't a
	// blanket "policy dir was passed" pass/fail but a per-document evaluation.
	if len(findings) != 2 {
		t.Fatalf("expected exactly 2 findings total (nothing from the Secret doc), got %d: %+v", len(findings), findings)
	}
}

func TestRun_NoPoliciesTriggeredIsClean(t *testing.T) {
	requireConftest(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.rego"), []byte(denyAndWarnPolicy), 0o644); err != nil {
		t.Fatal(err)
	}

	docs := []rules.ManifestDoc{
		{Kind: "Secret", Name: "clean", Text: "kind: Secret\nmetadata:\n  name: clean\n"},
	}
	findings, err := policyeval.Run(dir, docs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no document matches the policy, got %d: %+v", len(findings), findings)
	}
}

func TestRun_ConftestNotOnPathFailsClearly(t *testing.T) {
	// Simulate conftest missing by running with a PATH that excludes wherever it
	// actually lives, rather than uninstalling it globally.
	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })

	realConftest, lookErr := exec.LookPath("conftest")
	if lookErr == nil {
		// Build a PATH with every directory EXCEPT the one containing conftest.
		conftestDir := filepath.Dir(realConftest)
		var kept []string
		for _, p := range strings.Split(origPath, string(os.PathListSeparator)) {
			if p != conftestDir {
				kept = append(kept, p)
			}
		}
		os.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
	}
	// If conftest was never installed, origPath already lacks it and this test still
	// exercises the same not-found path.

	_, err := policyeval.Run(t.TempDir(), []rules.ManifestDoc{{Kind: "ConfigMap", Name: "x", Text: "kind: ConfigMap\n"}})
	if err == nil {
		t.Fatal("expected an error when conftest is not on PATH")
	}
	if !strings.Contains(err.Error(), "conftest not found on PATH") {
		t.Fatalf("expected a clear 'conftest not found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "conftest.dev") {
		t.Fatalf("expected the error to point at install instructions, got: %v", err)
	}
}

func TestRun_EmptyDocsIsNoop(t *testing.T) {
	// conftest is still required even with zero docs -- a --policy-dir the operator
	// asked for should behave consistently regardless of how many manifests happened
	// to render this run, not silently succeed only when the doc count is zero.
	requireConftest(t)
	findings, err := policyeval.Run(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("expected no error for empty docs, got: %v", err)
	}
	if findings != nil {
		t.Fatalf("expected nil findings for empty docs, got: %+v", findings)
	}
}
