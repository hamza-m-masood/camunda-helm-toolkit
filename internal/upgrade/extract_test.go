package upgrade

import "testing"

// The fixture under testdata/charts mirrors the real chart's constraints.tpl shapes:
// helper doc comments containing literal "Usage:" call sites, a $variable migration
// target, and each condition form the classifier has to recognise.
func extractFixture(t *testing.T) Manifest {
	t.Helper()
	m, err := Extract("testdata", Line{9, 1})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return m
}

func findKey(m Manifest, old string) (Key, bool) {
	for _, k := range m.Keys {
		if k.Old == old {
			return k, true
		}
	}
	return Key{}, false
}

func TestExtractSkipsHelperDocComments(t *testing.T) {
	m := extractFixture(t)
	// This key only appears inside a {{/* ... */}} Usage example. Picking it up would
	// invent a migration the chart never asks for.
	if k, ok := findKey(m, "doc.example.shouldBeIgnored"); ok {
		t.Fatalf("extracted a key from a doc comment: %+v", k)
	}
}

func TestExtractRenamed(t *testing.T) {
	m := extractFixture(t)
	k, ok := findKey(m, "identity.keycloak")
	if !ok {
		t.Fatal("identity.keycloak not extracted")
	}
	if k.Action != ActionRenamed || k.New != "identityKeycloak" {
		t.Fatalf("got action=%s new=%q, want renamed -> identityKeycloak", k.Action, k.New)
	}
	if k.Trigger != TriggerTruthy {
		t.Errorf("trigger = %s, want truthy", k.Trigger)
	}
}

func TestExtractRemovedUsesPresenceTrigger(t *testing.T) {
	m := extractFixture(t)
	k, ok := findKey(m, "global.license.key")
	if !ok {
		t.Fatal("global.license.key not extracted")
	}
	if k.Action != ActionRemoved {
		t.Errorf("action = %s, want removed", k.Action)
	}
	if k.Trigger != TriggerPresence {
		t.Errorf("trigger = %s, want presence (condition uses hasKey)", k.Trigger)
	}
}

func TestExtractCompoundConditionIsUnknown(t *testing.T) {
	m := extractFixture(t)
	k, ok := findKey(m, "compound.condition.key")
	if !ok {
		t.Fatal("compound.condition.key not extracted")
	}
	// A condition the classifier cannot model must degrade to "unknown" so the planner
	// marks the finding approximate rather than asserting a precise answer.
	if k.Trigger != TriggerUnknown {
		t.Errorf("trigger = %s, want unknown for an `and` condition", k.Trigger)
	}
}

func TestExtractDeprecatedTriggers(t *testing.T) {
	m := extractFixture(t)
	for _, tc := range []struct {
		key         string
		wantTrigger Trigger
		wantDefault string
	}{
		{"orchestration.logLevel", TriggerNotDefault, "info"},
		{"orchestration.index.prefix", TriggerNonEmpty, ""},
		{"orchestration.history.retention.enabled", TriggerTruthy, ""},
		{"orchestration.security.authorizations.enabled", TriggerFalsy, ""},
	} {
		k, ok := findKey(m, tc.key)
		if !ok {
			t.Errorf("%s not extracted", tc.key)
			continue
		}
		if k.Action != ActionDeprecated {
			t.Errorf("%s action = %s, want deprecated", tc.key, k.Action)
		}
		if k.Trigger != tc.wantTrigger {
			t.Errorf("%s trigger = %s, want %s", tc.key, k.Trigger, tc.wantTrigger)
		}
		if k.Default != tc.wantDefault {
			t.Errorf("%s default = %q, want %q", tc.key, k.Default, tc.wantDefault)
		}
	}
}

func TestExtractResolvesMigrationVariable(t *testing.T) {
	m := extractFixture(t)
	k, _ := findKey(m, "orchestration.logLevel")
	// The chart passes $orchestrationExtra rather than a literal; an unresolved "$..."
	// would surface to the user as advice to configure a key that does not exist.
	if k.Migration != "orchestration.extraConfiguration" {
		t.Fatalf("migration = %q, want orchestration.extraConfiguration", k.Migration)
	}
}

func TestExtractMetadata(t *testing.T) {
	m := extractFixture(t)
	if m.ChartMajor != 99 {
		t.Errorf("ChartMajor = %d, want 99 (from Chart.yaml)", m.ChartMajor)
	}
	if m.RequiresHelmMajor != 4 {
		t.Errorf("RequiresHelmMajor = %d, want 4", m.RequiresHelmMajor)
	}
	if m.Support != "supportStandard" {
		t.Errorf("Support = %q, want supportStandard", m.Support)
	}
	for _, k := range m.ByActionRemovedIn() {
		if k.RemovedIn != "100" {
			t.Errorf("%s RemovedIn = %q, want 100", k.Old, k.RemovedIn)
		}
	}
}

func TestExtractRecordsSource(t *testing.T) {
	m := extractFixture(t)
	k, _ := findKey(m, "global.license.key")
	if k.Source == "" {
		t.Fatal("Source is empty; a finding with no traceable origin is not auditable")
	}
}
