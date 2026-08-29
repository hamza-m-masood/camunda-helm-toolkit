package upgrade

import (
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := LoadStore()
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	return s
}

func findByRule(fs []rules.Finding, ruleID string) []rules.Finding {
	var out []rules.Finding
	for _, f := range fs {
		if f.RuleID == ruleID {
			out = append(out, f)
		}
	}
	return out
}

func TestPlanEmptyValuesProducesNoKeyFindings(t *testing.T) {
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: values.Values{}}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	// An operator who supplied nothing has no removed/renamed/deprecated keys to fix.
	// This is the guard against reporting the chart's own defaults back at the user.
	for _, id := range []string{RuleRemovedKey, RuleRenamedKey, RuleDeprecatedKey} {
		if got := findByRule(p.Findings, id); len(got) > 0 {
			t.Errorf("%s fired on empty values: %+v", id, got[0])
		}
	}
}

func TestPlanDetectsRemovedKey(t *testing.T) {
	// global.license.key is removed in 8.10 with a hasKey condition.
	v := values.Values{"global": map[string]interface{}{
		"license": map[string]interface{}{"key": "abc123"},
	}}
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	found := findByRule(p.Findings, RuleRemovedKey)
	var hit bool
	for _, f := range found {
		if f.Path == "global.license.key" {
			hit = true
			if f.Severity != rules.High {
				t.Errorf("severity = %s, want HIGH", f.Severity)
			}
		}
	}
	if !hit {
		t.Fatalf("global.license.key not reported as removed; got %d removed findings", len(found))
	}
}

func TestPlanStripRemovedIsOptIn(t *testing.T) {
	mk := func() values.Values {
		return values.Values{"global": map[string]interface{}{
			"license": map[string]interface{}{"key": "abc123"},
		}}
	}
	kept, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: mk()}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values.GetPath(kept.MigratedValues, "global.license.key"); !ok {
		t.Error("removed key was deleted without --strip-removed; dropping a setting is the operator's call")
	}
	stripped, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: mk(), StripRemoved: true}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values.GetPath(stripped.MigratedValues, "global.license.key"); ok {
		t.Error("--strip-removed did not delete the removed key")
	}
}

func TestPlanDoesNotMutateInput(t *testing.T) {
	v := values.Values{"identity": map[string]interface{}{"keycloak": map[string]interface{}{"enabled": true}}}
	if _, err := (Request{From: Line{8, 7}, To: Line{8, 8}, UserValues: v, StripRemoved: true}).Plan(testStore(t)); err != nil {
		t.Fatal(err)
	}
	if _, ok := values.GetPath(v, "identity.keycloak.enabled"); !ok {
		t.Fatal("Plan mutated the caller's values")
	}
}

func TestPlanSubtreeRenameMovesLeaves(t *testing.T) {
	// 8.10 renames camundaHub.webModeler.* -> camundaHub.*
	v := values.Values{"camundaHub": map[string]interface{}{
		"webModeler": map[string]interface{}{
			"restapi": map[string]interface{}{"replicas": 2},
		},
	}}
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := values.GetPath(p.MigratedValues, "camundaHub.restapi.replicas"); !ok || got != 2 {
		t.Fatalf("subtree rename not applied: %#v", p.MigratedValues)
	}
	if len(findByRule(p.Findings, RuleRenamedKey)) == 0 {
		t.Error("subtree rename produced no finding")
	}
}

func TestPlanMultiHopReportsEveryHop(t *testing.T) {
	p, err := Request{From: Line{8, 7}, To: Line{8, 10}, UserValues: values.Values{}}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hops) != 3 {
		t.Fatalf("hops = %v, want 3", p.Hops)
	}
	if len(findByRule(p.Findings, RuleMinorSkip)) != 1 {
		t.Error("multi-hop upgrade did not raise the minor-skip finding")
	}
}

func TestPlanSameVersionIsNoop(t *testing.T) {
	p, err := Request{From: Line{8, 10}, To: Line{8, 10}, UserValues: values.Values{}}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Hops) != 0 || len(p.Findings) != 0 {
		t.Fatalf("planning a no-op produced hops=%v findings=%d", p.Hops, len(p.Findings))
	}
}

func TestPlanRejectsDowngrade(t *testing.T) {
	if _, err := (Request{From: Line{8, 10}, To: Line{8, 8}, UserValues: values.Values{}}).Plan(testStore(t)); err == nil {
		t.Fatal("Plan accepted a downgrade")
	}
}

func TestPlanBundledBitnamiIsCritical(t *testing.T) {
	v := values.Values{"identityKeycloak": map[string]interface{}{"enabled": true}}
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	got := findByRule(p.Findings, RuleBundledBitnami)
	if len(got) != 1 {
		t.Fatalf("bundled-Bitnami findings = %d, want 1", len(got))
	}
	if got[0].Severity != rules.Critical {
		t.Errorf("severity = %s, want CRITICAL (data loss if upgraded blind)", got[0].Severity)
	}
	if !p.Facts.BundledKeycloak {
		t.Error("Facts.BundledKeycloak not set")
	}
	// The runbook step gated on this fact has to actually appear.
	var hasStep bool
	for _, s := range p.Runbook {
		if s.ID == "bitnami-offload" {
			hasStep = true
		}
	}
	if !hasStep {
		t.Error("bitnami-offload runbook step missing despite bundled Keycloak")
	}
}

func TestPlanBundledBitnamiNotRaisedBelow810(t *testing.T) {
	v := values.Values{"identityKeycloak": map[string]interface{}{"enabled": true}}
	p, err := Request{From: Line{8, 8}, To: Line{8, 9}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := findByRule(p.Findings, RuleBundledBitnami); len(got) != 0 {
		t.Errorf("bundled-Bitnami raised for an 8.9 target, where the subcharts still ship: %+v", got[0])
	}
	for _, s := range p.Runbook {
		if s.ID == "bitnami-offload" {
			t.Error("bitnami-offload step shown for a hop that does not remove the subcharts")
		}
	}
}

func TestPlanEscapeHatchDetected(t *testing.T) {
	v := values.Values{"orchestration": map[string]interface{}{
		"env": []interface{}{map[string]interface{}{"name": "X", "value": "y"}},
	}}
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	got := findByRule(p.Findings, RuleEscapeHatch)
	if len(got) != 1 || got[0].Path != "orchestration.env" {
		t.Fatalf("escape-hatch findings = %+v, want one for orchestration.env", got)
	}
}

func TestPlanEmptyEscapeHatchIsNotReported(t *testing.T) {
	v := values.Values{"orchestration": map[string]interface{}{"env": []interface{}{}}}
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: v}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := findByRule(p.Findings, RuleEscapeHatch); len(got) != 0 {
		t.Errorf("empty env reported as an escape hatch: %+v", got[0])
	}
}

func TestPlanMissingHopDataIsReported(t *testing.T) {
	// 8.6 and 8.7 predate the chart's structured deprecation helpers, so no manifest is
	// embedded for them. The plan must say so instead of implying a clean hop.
	p, err := Request{From: Line{8, 6}, To: Line{8, 8}, UserValues: values.Values{}}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(findByRule(p.Findings, RuleNoData)) == 0 {
		t.Fatal("no-data finding missing for the 8.7 hop")
	}
}

func TestPlanRunbookAlwaysIncludesCommonSteps(t *testing.T) {
	p, err := Request{From: Line{8, 9}, To: Line{8, 10}, UserValues: values.Values{}}.Plan(testStore(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"snapshot-values": false, "dry-run": false, "immutable-field-contingency": false}
	for _, s := range p.Runbook {
		if _, ok := want[s.ID]; ok {
			want[s.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("common runbook step %q missing", id)
		}
	}
}

func TestRunbookContingenciesSortLast(t *testing.T) {
	steps, err := RunbookFor([]string{"8.10"}, Facts{BundledKeycloak: true}, Substitutions{})
	if err != nil {
		t.Fatal(err)
	}
	seenContingency := false
	for _, s := range steps {
		if s.Kind == StepContingency {
			seenContingency = true
			continue
		}
		if seenContingency {
			t.Fatal("a non-contingency step follows a contingency; destructive remedies must read as footnotes")
		}
	}
}

func TestRunbookSubstitutesPlaceholders(t *testing.T) {
	steps, err := RunbookFor([]string{"8.10"}, Facts{}, Substitutions{Release: "camunda", Namespace: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		for _, c := range s.Commands {
			if strings.Contains(c, "{{") {
				t.Errorf("unsubstituted placeholder in %s: %s", s.ID, c)
			}
		}
	}
}
