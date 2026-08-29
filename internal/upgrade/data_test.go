package upgrade

import (
	"strings"
	"testing"
)

// These guard the generated data itself. Because the manifests are produced by parsing
// the chart, a parser regression or an upstream refactor shows up here as empty or
// malformed data rather than as silently missing findings in the field.
func TestEmbeddedDataIsNotEmpty(t *testing.T) {
	store := testStore(t)
	lines := store.Lines()
	if len(lines) == 0 {
		t.Fatal("no lines embedded")
	}
	for _, l := range lines {
		m, _ := store.Get(l)
		if len(m.Keys) == 0 {
			t.Errorf("%s embeds zero keys; an empty manifest silently claims the hop has no changes", l)
		}
		if m.ChartMajor == 0 {
			t.Errorf("%s has no chartMajor", l)
		}
	}
}

func TestEmbeddedKeysAreWellFormed(t *testing.T) {
	store := testStore(t)
	for _, l := range store.Lines() {
		m, _ := store.Get(l)
		for _, k := range m.Keys {
			if k.Old == "" {
				t.Errorf("%s: key with empty old name", l)
			}
			if strings.HasPrefix(k.Old, "$") || strings.HasPrefix(k.Migration, "$") {
				t.Errorf("%s: unresolved template variable in %+v", l, k)
			}
			if k.Action == ActionRenamed && k.New == "" {
				t.Errorf("%s: %s is renamed but names no replacement", l, k.Old)
			}
			if k.Trigger == "" {
				t.Errorf("%s: %s has no trigger", l, k.Old)
			}
			if k.Source == "" {
				t.Errorf("%s: %s has no source; findings must be traceable", l, k.Old)
			}
			// A subtree rename has to point at a subtree, or MoveSubtree would move a
			// leaf into a path that reads as a subtree root.
			if k.IsSubtree() && k.Action == ActionRenamed && !strings.HasSuffix(k.New, ".*") {
				t.Errorf("%s: subtree key %s renames to non-subtree %s", l, k.Old, k.New)
			}
		}
	}
}

func TestLatestLineIs810OrNewer(t *testing.T) {
	latest := testStore(t).Latest()
	if latest.Compare(Line{8, 10}) < 0 {
		t.Fatalf("Latest() = %s; the default target should be the newest known line", latest)
	}
}

func TestExpectedKeysArePresent(t *testing.T) {
	store := testStore(t)
	// Spot-checks against the real chart, so a parser change that quietly stops finding
	// call sites fails here instead of in a customer's upgrade.
	for _, tc := range []struct {
		line   Line
		key    string
		action Action
	}{
		{Line{8, 10}, "global.license.key", ActionRemoved},
		{Line{8, 10}, "camundaHub.webModeler.*", ActionRenamed},
		{Line{8, 10}, "orchestration.logLevel", ActionDeprecated},
		{Line{8, 8}, "identity.keycloak", ActionRenamed},
	} {
		m, ok := store.Get(tc.line)
		if !ok {
			t.Errorf("no manifest for %s", tc.line)
			continue
		}
		var found bool
		for _, k := range m.Keys {
			if k.Old == tc.key && k.Action == tc.action {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: expected %s as %s, not found", tc.line, tc.key, tc.action)
		}
	}
}
