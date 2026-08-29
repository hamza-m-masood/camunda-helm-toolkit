package rules_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

func TestCheckOrchestrationGracePeriodAndHeapDump(t *testing.T) {
	noGraceHeapOnData := rules.ManifestDoc{
		Kind: "StatefulSet",
		Name: "release-zeebe",
		Text: `
kind: StatefulSet
metadata:
  name: release-zeebe
spec:
  template:
    spec:
      containers:
        - env:
            - name: JAVA_TOOL_OPTIONS
              value: "-XX:HeapDumpPath=/usr/local/camunda/data"
          volumeMounts:
            - name: data
              mountPath: /usr/local/camunda/data
`,
	}
	if f := rules.CheckOrchestrationGracePeriodAndHeapDump([]rules.ManifestDoc{noGraceHeapOnData}); len(f) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f))
	}

	fixed := rules.ManifestDoc{
		Kind: "StatefulSet",
		Name: "release-zeebe",
		Text: `
kind: StatefulSet
metadata:
  name: release-zeebe
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 180
      containers:
        - env:
            - name: JAVA_TOOL_OPTIONS
              value: "-XX:HeapDumpPath=/tmp"
          volumeMounts:
            - name: data
              mountPath: /usr/local/camunda/data
`,
	}
	if f := rules.CheckOrchestrationGracePeriodAndHeapDump([]rules.ManifestDoc{fixed}); len(f) != 0 {
		t.Errorf("expected 0 findings once both fixed, got %d: %+v", len(f), f)
	}

	notOrchestration := rules.ManifestDoc{Kind: "StatefulSet", Name: "some-postgres", Text: "kind: StatefulSet\nmetadata:\n  name: some-postgres\n"}
	if f := rules.CheckOrchestrationGracePeriodAndHeapDump([]rules.ManifestDoc{notOrchestration}); len(f) != 0 {
		t.Errorf("expected 0 findings for a StatefulSet with no HeapDumpPath at all, got %d", len(f))
	}
}
