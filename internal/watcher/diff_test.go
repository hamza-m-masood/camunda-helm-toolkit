package watcher_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/watcher"
)

func TestDiff_FirstRunEverythingIsNew(t *testing.T) {
	current := []rules.Finding{
		{RuleID: "CCD001", Path: "orchestration.podDisruptionBudget.enabled"},
		{RuleID: "CCD011", Path: ""},
	}
	newFindings := watcher.Diff(current, map[string]bool{})
	if len(newFindings) != 2 {
		t.Fatalf("expected both findings to be new on first run, got %d", len(newFindings))
	}
}

func TestDiff_SecondRunNoChangeReportsNothingNew(t *testing.T) {
	current := []rules.Finding{
		{RuleID: "CCD001", Path: "orchestration.podDisruptionBudget.enabled"},
	}
	prev := map[string]bool{watcher.Key(current[0]): true}
	newFindings := watcher.Diff(current, prev)
	if len(newFindings) != 0 {
		t.Fatalf("expected 0 new findings when nothing changed, got %d: %+v", len(newFindings), newFindings)
	}
}

func TestDiff_OnlyGenuinelyNewFindingReported(t *testing.T) {
	prevFinding := rules.Finding{RuleID: "CCD001", Path: "orchestration.podDisruptionBudget.enabled"}
	newFinding := rules.Finding{RuleID: "CCD011", Path: ""}
	current := []rules.Finding{prevFinding, newFinding}
	prev := map[string]bool{watcher.Key(prevFinding): true}

	got := watcher.Diff(current, prev)
	if len(got) != 1 || got[0].RuleID != "CCD011" {
		t.Fatalf("expected only CCD011 reported as new, got %+v", got)
	}
}

func TestKeysOf_RoundTripsThroughDiff(t *testing.T) {
	current := []rules.Finding{
		{RuleID: "CCD001", Path: "a"},
		{RuleID: "CCD002", Path: "b"},
	}
	keys := watcher.KeysOf(current)
	prev := map[string]bool{}
	for _, k := range keys {
		prev[k] = true
	}
	if got := watcher.Diff(current, prev); len(got) != 0 {
		t.Fatalf("expected KeysOf(current) fed back into Diff to report nothing new, got %+v", got)
	}
}
