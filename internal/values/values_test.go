package values_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func TestDeepMerge_MapsMergeArraysReplace(t *testing.T) {
	dst, err := values.ParseYAML([]byte(`
a:
  b: 1
  c: 2
list: [1, 2, 3]
`))
	if err != nil {
		t.Fatal(err)
	}
	src, err := values.ParseYAML([]byte(`
a:
  c: 99
  d: 3
list: [9]
`))
	if err != nil {
		t.Fatal(err)
	}
	merged := values.DeepMerge(dst, src)

	got, _ := values.GetPath(merged, "a.b")
	if got != 1 {
		t.Errorf("a.b: expected 1 (preserved from dst), got %v", got)
	}
	got, _ = values.GetPath(merged, "a.c")
	if got != 99 {
		t.Errorf("a.c: expected 99 (overridden by src), got %v", got)
	}
	got, _ = values.GetPath(merged, "a.d")
	if got != 3 {
		t.Errorf("a.d: expected 3 (added by src), got %v", got)
	}
	list, ok := merged["list"].([]interface{})
	if !ok || len(list) != 1 {
		t.Errorf("list: expected src's array to wholesale-replace dst's, got %v", merged["list"])
	}
}

func TestFindBlocks_FindsNestedMatchesAtAnyDepth(t *testing.T) {
	v, err := values.ParseYAML([]byte(`
a:
  resources:
    requests: {memory: "1Gi"}
    limits: {memory: "2Gi"}
b:
  nested:
    deeper:
      resources:
        requests: {memory: "1Gi"}
        limits: {memory: "2Gi"}
c:
  resources:
    requests: {memory: "1Gi"}
    # no limits here — should not match
`))
	if err != nil {
		t.Fatal(err)
	}
	blocks := values.FindBlocks(v, "requests", "limits")
	if len(blocks) != 2 {
		t.Fatalf("expected 2 matching blocks, got %d: %+v", len(blocks), blocks)
	}
}

func TestParseQuantity(t *testing.T) {
	cases := map[string]float64{
		"1500Mi": 1500 * (1 << 20),
		"3Gi":    3 * (1 << 30),
		"500m":   0.5,
		"2":      2,
	}
	for in, want := range cases {
		got, ok := values.ParseQuantity(in)
		if !ok {
			t.Errorf("ParseQuantity(%q): expected ok=true", in)
			continue
		}
		if got != want {
			t.Errorf("ParseQuantity(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestToInt_HandlesQuotedNumericStrings(t *testing.T) {
	// The Camunda chart quotes replicationFactor/clusterSize as strings in YAML.
	n, err := values.ToInt("3")
	if err != nil || n != 3 {
		t.Errorf("ToInt(\"3\") = %d, %v; want 3, nil", n, err)
	}
}

func TestGetPath_ResolvesArrayIndexSegments(t *testing.T) {
	v, err := values.ParseYAML([]byte(`
orchestration:
  security:
    initialization:
      users:
        - username: demo
          password: demo
        - username: connectors
          password: connector
`))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := values.GetPath(v, "orchestration.security.initialization.users[1].username")
	if !ok {
		t.Fatal("expected users[1].username to resolve")
	}
	if got != "connectors" {
		t.Errorf("expected \"connectors\", got %#v", got)
	}

	if _, ok := values.GetPath(v, "orchestration.security.initialization.users[5].username"); ok {
		t.Error("expected an out-of-range index to fail to resolve, not panic or return something")
	}
}
