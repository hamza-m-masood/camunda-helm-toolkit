package report_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/report"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
)

// A hand-rolled structural check, not a full SARIF 2.1.0 schema validation (that
// would mean vendoring the spec) — it asserts the shape a consumer like GitHub code
// scanning actually reads: schema/version, tool.driver.name, a complete rules
// catalog, and one result per finding with the right severity mapping.
func TestWriteSARIF_StructuralShape(t *testing.T) {
	findings := []rules.Finding{
		{RuleID: "CCD001", Severity: rules.High, Title: "t1", Path: "a.b"},
		{RuleID: "CCD002", Severity: rules.Critical, Title: "t2", Path: "c.d"},
		{RuleID: "CCD006", Severity: rules.Low, Title: "t3"},
	}
	var buf bytes.Buffer
	if err := report.WriteSARIF(&buf, findings); err != nil {
		t.Fatal(err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if doc["version"] != "2.1.0" {
		t.Errorf("version = %v, want 2.1.0", doc["version"])
	}
	if _, ok := doc["$schema"].(string); !ok {
		t.Error("$schema missing or not a string")
	}

	runs, ok := doc["runs"].([]interface{})
	if !ok || len(runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %v", doc["runs"])
	}
	run := runs[0].(map[string]interface{})

	driver := run["tool"].(map[string]interface{})["driver"].(map[string]interface{})
	if driver["name"] != "camunda-helm-toolkit" {
		t.Errorf("driver.name = %v", driver["name"])
	}
	rulesArr := driver["rules"].([]interface{})
	if len(rulesArr) != len(rules.AllRuleMetas()) {
		t.Errorf("expected the rules catalog to list every registered rule (%d), got %d", len(rules.AllRuleMetas()), len(rulesArr))
	}

	results := run["results"].([]interface{})
	if len(results) != len(findings) {
		t.Fatalf("expected %d results, got %d", len(findings), len(results))
	}
	levelByRule := map[string]string{}
	for _, r := range results {
		rm := r.(map[string]interface{})
		levelByRule[rm["ruleId"].(string)] = rm["level"].(string)
	}
	if levelByRule["CCD001"] != "error" { // High -> error
		t.Errorf("CCD001 level = %s, want error", levelByRule["CCD001"])
	}
	if levelByRule["CCD002"] != "error" { // Critical -> error
		t.Errorf("CCD002 level = %s, want error", levelByRule["CCD002"])
	}
	if levelByRule["CCD006"] != "note" { // Low -> note
		t.Errorf("CCD006 level = %s, want note", levelByRule["CCD006"])
	}
}
