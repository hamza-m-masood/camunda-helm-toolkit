package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exec the actual compiled binary (see init_test.go's TestMain/runBinary),
// the same standard every other new command/flag in this project is held to, against
// the real camunda-platform-8.9/8.10 charts checked out on this machine.

func TestCheckBinary_RulesFrom(t *testing.T) {
	skipIfChartMissing(t, realChart89)

	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.yaml")
	rules := `
rules:
  - id: CUSTOM001
    severity: high
    title: "orchestration.podDisruptionBudget is disabled (custom rule)"
    path: orchestration.podDisruptionBudget.enabled
    condition: equals
    value: "false"
  - id: CUSTOM002
    severity: low
    title: "should never fire against real chart defaults"
    path: orchestration.replicationFactor
    condition: equals
    value: "999"
`
	if err := os.WriteFile(rulesPath, []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}

	// Not asserting on exit code here: the real chart's own defaults already trip a
	// CRITICAL finding (CCD002, unrelated to this feature), so a nonzero exit is
	// expected regardless of whether the custom rule logic is correct — the content
	// assertions below are what actually prove this feature works.
	out, _ := runBinary("check", "--chart", realChart89, "--rules-from", rulesPath, "--no-color")
	if !strings.Contains(out, "CUSTOM001") {
		t.Errorf("expected CUSTOM001 to fire against real chart defaults (PDB disabled by default), got:\n%s", out)
	}
	if strings.Contains(out, "CUSTOM002") {
		t.Errorf("expected CUSTOM002 to NOT fire, got:\n%s", out)
	}
}

func TestCheckBinary_RulesFromRejectsReservedPrefix(t *testing.T) {
	skipIfChartMissing(t, realChart89)

	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(rulesPath, []byte(`
rules:
  - id: CCD999
    severity: high
    title: x
    path: a.b
    condition: exists
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBinary("check", "--chart", realChart89, "--rules-from", rulesPath)
	if err == nil {
		t.Fatal("expected a nonzero exit for a rule ID using the reserved CCD prefix")
	}
	if !strings.Contains(out, "reserved") {
		t.Errorf("expected a clear reserved-prefix error, got:\n%s", out)
	}
}

func TestCheckBinary_PolicyDir(t *testing.T) {
	skipIfChartMissing(t, realChart89)
	if _, err := exec.LookPath("conftest"); err != nil {
		t.Skip("conftest not on PATH")
	}

	overlayDir := t.TempDir()
	overlayPath := filepath.Join(overlayDir, "overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte("orchestration:\n  data:\n    secondaryStorage:\n      type: elasticsearch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(policyDir, "main.rego"), []byte(`package main

deny contains msg if {
	input.kind == "StatefulSet"
	msg := "policy fired against a real rendered StatefulSet"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBinary("check", "--chart", realChart89, "-f", overlayPath, "--policy-dir", policyDir, "--no-color", "--fail-on", "high")
	if err == nil {
		t.Fatalf("expected a nonzero exit (the policy denies), got clean output:\n%s", out)
	}
	if !strings.Contains(out, "POLICY-DENY") {
		t.Errorf("expected a POLICY-DENY finding, got:\n%s", out)
	}
}

func TestCheckBinary_PolicyDirRequiresChart(t *testing.T) {
	out, err := runBinary("check", "--release", "irrelevant", "--policy-dir", t.TempDir())
	if err == nil {
		t.Fatal("expected a nonzero exit when --policy-dir is set without --chart")
	}
	if !strings.Contains(out, "--policy-dir requires --chart") {
		t.Errorf("expected a clear --policy-dir-requires---chart error, got:\n%s", out)
	}
}

func TestCheckBinary_PolicyDirConftestMissing(t *testing.T) {
	skipIfChartMissing(t, realChart89)

	realConftest, lookErr := exec.LookPath("conftest")
	if lookErr != nil {
		t.Skip("conftest already absent from PATH — nothing to exclude for this test")
	}
	conftestDir := filepath.Dir(realConftest)

	overlayDir := t.TempDir()
	overlayPath := filepath.Join(overlayDir, "overlay.yaml")
	if err := os.WriteFile(overlayPath, []byte("orchestration:\n  data:\n    secondaryStorage:\n      type: elasticsearch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	var kept []string
	for _, p := range strings.Split(origPath, string(os.PathListSeparator)) {
		if p != conftestDir {
			kept = append(kept, p)
		}
	}
	restrictedPath := strings.Join(kept, string(os.PathListSeparator))

	cmd := exec.Command(testBinary, "check", "--chart", realChart89, "-f", overlayPath, "--policy-dir", t.TempDir())
	cmd.Env = append(os.Environ(), "PATH="+restrictedPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected a nonzero exit when conftest is not on PATH")
	}
	if !strings.Contains(string(out), "conftest not found on PATH") {
		t.Errorf("expected a clear conftest-not-found error, got:\n%s", out)
	}
}

// An adversarial audit found that the same custom rule id defined in two DIFFERENT
// --rules-from files was silently allowed -- each file only validates its own rules in
// isolation, so nothing ever compared ids across files the way the CCD-prefix
// collision already was. This proves the fix: a cross-file id collision is a clear
// load-time error, not two rules quietly reporting under one ambiguous id.
func TestCheckBinary_RulesFrom_DuplicateIDAcrossFilesIsRejected(t *testing.T) {
	skipIfChartMissing(t, realChart89)

	dir := t.TempDir()
	rule := `
rules:
  - id: CUSTOM001
    severity: low
    title: "duplicate"
    path: orchestration.replicationFactor
    condition: exists
`
	fileA := filepath.Join(dir, "a.yaml")
	fileB := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(fileA, []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runBinary("check", "--chart", realChart89, "--rules-from", fileA, "--rules-from", fileB, "--no-color")
	if err == nil {
		t.Fatal("expected a nonzero exit for a duplicate rule id across two --rules-from files")
	}
	if !strings.Contains(out, `id "CUSTOM001"`) || !strings.Contains(out, "already defined in") {
		t.Errorf("expected a clear duplicate-id error naming CUSTOM001 and the earlier source file, got:\n%s", out)
	}
}
