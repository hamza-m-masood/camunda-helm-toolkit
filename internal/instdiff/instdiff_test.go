package instdiff_test

import (
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/instdiff"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func parse(t *testing.T, y string) map[string]interface{} {
	t.Helper()
	v, err := values.ParseYAML([]byte(y))
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return map[string]interface{}(v)
}

func findChange(t *testing.T, changes []instdiff.Change, path string) instdiff.Change {
	t.Helper()
	for _, c := range changes {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no change found for path %q among %+v", path, changes)
	return instdiff.Change{}
}

func TestCompare_AddedRemovedChanged(t *testing.T) {
	a := parse(t, `
orchestration:
  clusterSize: "3"
  removedOnly: "gone-in-b"
identity:
  enabled: true
`)
	b := parse(t, `
orchestration:
  clusterSize: "5"
  addedOnly: "new-in-b"
identity:
  enabled: true
`)

	changes := instdiff.Compare(a, b)

	removed := findChange(t, changes, "orchestration.removedOnly")
	if removed.Kind != instdiff.Removed || removed.A != "gone-in-b" {
		t.Errorf("expected Removed with A=gone-in-b, got %+v", removed)
	}

	added := findChange(t, changes, "orchestration.addedOnly")
	if added.Kind != instdiff.Added || added.B != "new-in-b" {
		t.Errorf("expected Added with B=new-in-b, got %+v", added)
	}

	changed := findChange(t, changes, "orchestration.clusterSize")
	if changed.Kind != instdiff.Changed || changed.A != "3" || changed.B != "5" {
		t.Errorf("expected Changed 3 -> 5, got %+v", changed)
	}

	// identity.enabled is identical on both sides -- must not appear at all.
	for _, c := range changes {
		if c.Path == "identity.enabled" {
			t.Errorf("expected no change reported for an identical key, got %+v", c)
		}
	}
}

func TestCompare_IsStructuralNotTextual_KeyOrderIndependent(t *testing.T) {
	// Same content, keys written in a different order -- values.ParseYAML decodes
	// both into a Go map, which has no inherent order, so a structural diff over
	// maps is naturally immune to this; a line-based text diff of the two raw YAML
	// documents would NOT be, since key order differs in the source text.
	a := parse(t, "a: 1\nb: 2\nc: 3\n")
	b := parse(t, "c: 3\na: 1\nb: 2\n")
	if changes := instdiff.Compare(a, b); len(changes) != 0 {
		t.Fatalf("expected 0 changes for reordered-but-identical keys, got %+v", changes)
	}
}

func TestCompare_QuotedVsUnquotedScalarIsARealTypeDifference(t *testing.T) {
	// This is NOT a "same value, different formatting" case: YAML `true` decodes as
	// a Go bool, `"true"` decodes as a Go string -- Helm treats them differently too
	// (a values.yaml field expecting a bool gets a template type error from a quoted
	// string), so a structural diff correctly reports this as Changed, not silently
	// unifying them the way a human eyeballing the values might assume it should.
	a := parse(t, "flag: true\n")
	b := parse(t, `flag: "true"`+"\n")
	changes := instdiff.Compare(a, b)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change (bool vs string are genuinely different types), got %+v", changes)
	}
	if changes[0].Kind != instdiff.Changed {
		t.Errorf("expected Changed, got %+v", changes[0])
	}
}

func TestCompare_NestedMapsRecurse(t *testing.T) {
	a := parse(t, `
orchestration:
  resources:
    requests:
      memory: "1Gi"
    limits:
      memory: "2Gi"
`)
	b := parse(t, `
orchestration:
  resources:
    requests:
      memory: "2Gi"
    limits:
      memory: "2Gi"
`)
	changes := instdiff.Compare(a, b)
	if len(changes) != 1 {
		t.Fatalf("expected exactly 1 change, got %+v", changes)
	}
	if changes[0].Path != "orchestration.resources.requests.memory" {
		t.Errorf("expected the nested path to be reported precisely, got %s", changes[0].Path)
	}
}

func TestCompare_ArraysAreOpaqueValuesNotElementDiffed(t *testing.T) {
	// Helm's own semantics: an overlay replaces an array wholesale, it never merges
	// element-by-element -- a diff at the array level (not "index 2 differs") is the
	// operationally meaningful unit here.
	a := parse(t, "list:\n  - a\n  - b\n")
	b := parse(t, "list:\n  - a\n  - b\n  - c\n")
	changes := instdiff.Compare(a, b)
	if len(changes) != 1 || changes[0].Path != "list" || changes[0].Kind != instdiff.Changed {
		t.Fatalf("expected exactly 1 Changed at path 'list', got %+v", changes)
	}
}

func TestCompare_IdenticalTreesProduceNoChanges(t *testing.T) {
	a := parse(t, "a: {b: 1, c: [1,2,3]}")
	b := parse(t, "a: {b: 1, c: [1,2,3]}")
	if changes := instdiff.Compare(a, b); len(changes) != 0 {
		t.Fatalf("expected 0 changes for identical trees, got %+v", changes)
	}
}

func TestWriteText_FormatsEachKind(t *testing.T) {
	changes := []instdiff.Change{
		{Path: "x.added", Kind: instdiff.Added, B: "new"},
		{Path: "x.removed", Kind: instdiff.Removed, A: "old"},
		{Path: "x.changed", Kind: instdiff.Changed, A: "1", B: "2"},
	}
	out := instdiff.WriteText(changes, "release-a", "release-b")
	for _, want := range []string{"+ x.added: new", "- x.removed: old", "~ x.changed: 1 -> 2", "release-a", "release-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestWriteText_NoChanges(t *testing.T) {
	out := instdiff.WriteText(nil, "a", "b")
	if !strings.Contains(out, "No differences") {
		t.Errorf("expected a clean 'no differences' message, got: %s", out)
	}
}
