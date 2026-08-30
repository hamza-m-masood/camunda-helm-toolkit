package customrules_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/customrules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func mustParse(t *testing.T, yamlStr string) values.Values {
	t.Helper()
	v, err := values.ParseYAML([]byte(yamlStr))
	if err != nil {
		t.Fatalf("parsing fixture yaml: %v", err)
	}
	return v
}

func TestParse_RejectsMissingID(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - severity: high
    title: x
    path: a.b
    condition: exists
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "missing required field id") {
		t.Fatalf("expected a missing-id error, got: %v", err)
	}
}

func TestParse_RejectsReservedIDPrefix(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CCD999
    severity: high
    title: x
    path: a.b
    condition: exists
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected a reserved-prefix error, got: %v", err)
	}
}

func TestParse_RejectsInvalidSeverity(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: catastrophic
    title: x
    path: a.b
    condition: exists
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "severity must be") {
		t.Fatalf("expected an invalid-severity error, got: %v", err)
	}
}

func TestParse_RejectsInvalidCondition(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: x
    path: a.b
    condition: isPrime
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "condition must be") {
		t.Fatalf("expected an invalid-condition error, got: %v", err)
	}
}

func TestParse_RejectsEqualsWithoutValue(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: x
    path: a.b
    condition: equals
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "requires a non-empty value") {
		t.Fatalf("expected a missing-value error, got: %v", err)
	}
}

func TestParse_RejectsInvalidRegex(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: x
    path: a.b
    condition: matches
    value: "(unterminated"
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "invalid regex") {
		t.Fatalf("expected an invalid-regex error, got: %v", err)
	}
}

func TestParse_RejectsMissingPath(t *testing.T) {
	_, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: x
    condition: exists
`), "test.yaml")
	if err == nil || !strings.Contains(err.Error(), "missing required field path") {
		t.Fatalf("expected a missing-path error, got: %v", err)
	}
}

func TestEvaluate_Exists(t *testing.T) {
	f, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: "orchestration.foo is set"
    path: orchestration.foo
    condition: exists
`), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	v := mustParse(t, `orchestration: {foo: "bar"}`)
	findings := f.Evaluate(v)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].RuleID != "CUSTOM001" {
		t.Errorf("expected RuleID CUSTOM001, got %s", findings[0].RuleID)
	}

	vAbsent := mustParse(t, `orchestration: {}`)
	if got := f.Evaluate(vAbsent); len(got) != 0 {
		t.Errorf("expected no findings when path is absent, got %+v", got)
	}
}

func TestEvaluate_Absent(t *testing.T) {
	f, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: medium
    title: "orchestration.foo is not set"
    path: orchestration.foo
    condition: absent
`), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Evaluate(mustParse(t, `orchestration: {}`)); len(got) != 1 {
		t.Fatalf("expected 1 finding when path is absent, got %d", len(got))
	}
	if got := f.Evaluate(mustParse(t, `orchestration: {foo: "bar"}`)); len(got) != 0 {
		t.Fatalf("expected 0 findings when path is present, got %d", len(got))
	}
}

func TestEvaluate_EqualsAndNotEquals(t *testing.T) {
	f, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: "must equal foo"
    path: a.b
    condition: equals
    value: "foo"
  - id: CUSTOM002
    severity: high
    title: "must not equal bar"
    path: a.b
    condition: notEquals
    value: "bar"
`), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}

	got := f.Evaluate(mustParse(t, `a: {b: "foo"}`))
	ids := map[string]bool{}
	for _, x := range got {
		ids[x.RuleID] = true
	}
	if !ids["CUSTOM001"] {
		t.Error("expected CUSTOM001 (equals foo) to fire when b == foo")
	}
	if !ids["CUSTOM002"] {
		t.Error("expected CUSTOM002 (notEquals bar) to fire when b == foo (foo != bar)")
	}

	got2 := f.Evaluate(mustParse(t, `a: {b: "bar"}`))
	ids2 := map[string]bool{}
	for _, x := range got2 {
		ids2[x.RuleID] = true
	}
	if ids2["CUSTOM001"] {
		t.Error("expected CUSTOM001 (equals foo) NOT to fire when b == bar")
	}
	if ids2["CUSTOM002"] {
		t.Error("expected CUSTOM002 (notEquals bar) NOT to fire when b == bar")
	}

	// notEquals does not fire when the path is simply absent -- absence is its own
	// condition, not an implicit case of "not equal to anything".
	got3 := f.Evaluate(mustParse(t, `a: {}`))
	for _, x := range got3 {
		if x.RuleID == "CUSTOM002" {
			t.Error("expected notEquals to NOT fire when the path is absent")
		}
	}
}

func TestEvaluate_Matches(t *testing.T) {
	f, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: low
    title: "release name looks like a test release"
    path: a.releaseName
    condition: matches
    value: "^test-.*"
`), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Evaluate(mustParse(t, `a: {releaseName: "test-foo"}`)); len(got) != 1 {
		t.Fatalf("expected the regex to match test-foo, got %d findings", len(got))
	}
	if got := f.Evaluate(mustParse(t, `a: {releaseName: "prod"}`)); len(got) != 0 {
		t.Fatalf("expected the regex to not match prod, got %d findings", len(got))
	}
}

func TestEvaluate_ArrayIndexPath(t *testing.T) {
	// Confirms Path really does support the same "[N]" syntax as GetPath, not a
	// separately-invented subset of it.
	f, err := customrules.Parse([]byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: "first user is demo"
    path: "orchestration.security.initialization.users[0].username"
    condition: equals
    value: "demo"
`), "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	v := mustParse(t, `
orchestration:
  security:
    initialization:
      users:
        - username: demo
          password: demo
`)
	if got := f.Evaluate(v); len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
}

func TestLoadFrom_LocalFileAndHTTP(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rules.yaml"
	content := []byte(`
rules:
  - id: CUSTOM001
    severity: high
    title: "test rule"
    path: a.b
    condition: exists
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	fromFile, err := customrules.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(local file): %v", err)
	}
	if len(fromFile.Rules) != 1 {
		t.Fatalf("expected 1 rule from local file, got %d", len(fromFile.Rules))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	fromHTTP, err := customrules.LoadFrom(srv.URL)
	if err != nil {
		t.Fatalf("LoadFrom(http): %v", err)
	}
	if len(fromHTTP.Rules) != 1 {
		t.Fatalf("expected 1 rule from HTTP, got %d", len(fromHTTP.Rules))
	}
}

func TestLoadFrom_HTTPNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := customrules.LoadFrom(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected a 404 status error, got: %v", err)
	}
}
