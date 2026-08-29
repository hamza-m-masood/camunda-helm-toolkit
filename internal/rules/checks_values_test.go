package rules_test

import (
	"os"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func loadFixture(t *testing.T, path string) values.Values {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	v, err := values.ParseYAML(data)
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	return v
}

func runAll(v values.Values) []rules.Finding {
	var out []rules.Finding
	for _, check := range rules.AllValuesChecks() {
		out = append(out, check(v)...)
	}
	return out
}

func findingIDs(findings []rules.Finding) map[string]int {
	m := map[string]int{}
	for _, f := range findings {
		m[f.RuleID]++
	}
	return m
}

func TestBadFixtureTripsEveryRule(t *testing.T) {
	v := loadFixture(t, "../../testdata/values/bad.yaml")
	findings := runAll(v)
	ids := findingIDs(findings)

	want := []string{
		"CCD001", "CCD002", "CCD003", "CCD004", "CCD005", "CCD006", "CCD007", "CCD008",
		"CCD013", "CCD014", "CCD016",
	}
	for _, id := range want {
		if ids[id] == 0 {
			t.Errorf("expected rule %s to fire on bad.yaml, but it did not (findings: %+v)", id, findings)
		}
	}
	// CCD014 should fire twice — both bundled Postgres subcharts are enabled with backup off.
	if ids["CCD014"] != 2 {
		t.Errorf("expected CCD014 to fire twice (two bundled subcharts), got %d", ids["CCD014"])
	}
	// CCD004 should fire twice — both demo/demo and connectors/connector are present.
	if ids["CCD004"] != 2 {
		t.Errorf("expected CCD004 to fire twice (two default users), got %d", ids["CCD004"])
	}
	// CCD005 should fire twice — both retention gates are off.
	if ids["CCD005"] != 2 {
		t.Errorf("expected CCD005 to fire twice (two retention gates), got %d", ids["CCD005"])
	}
}

func TestGoodFixtureIsClean(t *testing.T) {
	v := loadFixture(t, "../../testdata/values/good.yaml")
	findings := runAll(v)
	if len(findings) != 0 {
		t.Fatalf("expected zero findings on good.yaml, got %d: %+v", len(findings), findings)
	}
}

func TestCheckMemoryBurstable_SeverityByComponent(t *testing.T) {
	v, err := values.ParseYAML([]byte(`
orchestration:
  resources:
    requests: {memory: "1Gi"}
    limits:   {memory: "2Gi"}
identity:
  resources:
    requests: {memory: "1Gi"}
    limits:   {memory: "2Gi"}
`))
	if err != nil {
		t.Fatal(err)
	}
	findings := rules.CheckMemoryBurstable(v)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	sevByPath := map[string]rules.Severity{}
	for _, f := range findings {
		sevByPath[f.Path] = f.Severity
	}
	if sevByPath["orchestration.resources"] != rules.Critical {
		t.Errorf("expected orchestration mismatch to be Critical, got %s", sevByPath["orchestration.resources"])
	}
	if sevByPath["identity.resources"] != rules.Medium {
		t.Errorf("expected identity mismatch to be Medium, got %s", sevByPath["identity.resources"])
	}
}

func TestCheckReplicationFactor_OnlyFiresWhenExceeding(t *testing.T) {
	ok, err := values.ParseYAML([]byte(`orchestration: {replicationFactor: "3", clusterSize: "3"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := rules.CheckReplicationFactor(ok); len(f) != 0 {
		t.Errorf("expected no findings when replicationFactor == clusterSize, got %+v", f)
	}

	bad, err := values.ParseYAML([]byte(`orchestration: {replicationFactor: "3", clusterSize: "1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := rules.CheckReplicationFactor(bad); len(f) != 1 {
		t.Errorf("expected 1 finding when replicationFactor > clusterSize, got %+v", f)
	}
}
