package suppress_test

import (
	"os"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/suppress"
)

func TestLoad_RejectsMissingRuleIDOrReason(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "ignore-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()

	cases := []string{
		"suppress:\n  - path: a.b\n    reason: no rule id\n",
		"suppress:\n  - ruleId: CCD001\n",
	}
	for _, content := range cases {
		if err := os.WriteFile(tmp.Name(), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := suppress.Load(tmp.Name()); err == nil {
			t.Errorf("expected Load to reject %q, got no error", content)
		}
	}
}

func TestApply_MatchesByRuleIDAndOptionalPathPrefix(t *testing.T) {
	f, err := suppress.Load("../../testdata/suppress/ignore.yaml")
	if err != nil {
		t.Fatal(err)
	}

	findings := []rules.Finding{
		{RuleID: "CCD002", Path: "identity.resources"},           // suppressed: exact path match
		{RuleID: "CCD002", Path: "identity.resources.requests"},  // suppressed: path-prefix match
		{RuleID: "CCD002", Path: "orchestration.resources"},      // kept: same rule, different path
		{RuleID: "CCD004", Path: "orchestration.security.users"}, // suppressed: rule-only entry, any path
		{RuleID: "CCD001"}, // kept: no matching entry at all
	}

	kept, suppressed := f.Apply(findings)
	if len(suppressed) != 3 {
		t.Fatalf("expected 3 suppressed, got %d: %+v", len(suppressed), suppressed)
	}
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d: %+v", len(kept), kept)
	}
	if kept[0].RuleID != "CCD002" || kept[0].Path != "orchestration.resources" {
		t.Errorf("unexpected kept[0]: %+v", kept[0])
	}
	if kept[1].RuleID != "CCD001" {
		t.Errorf("unexpected kept[1]: %+v", kept[1])
	}
}

func TestLoad_MissingFileReturnsOSError(t *testing.T) {
	_, err := suppress.Load("does-not-exist.yaml")
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected an os.IsNotExist error, got %v", err)
	}
}
