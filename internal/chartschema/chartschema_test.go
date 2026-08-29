package chartschema

import (
	"os"
	"strings"
	"testing"
)

const fixture = `
global:
  ## @extra global.license
  license:
    ## @extra global.license.secret configuration to provide the license secret.
    secret:
      ## @param global.license.secret.inlineSecret can be used to provide the license as a plain-text value.
      inlineSecret: ""
      ## @param global.license.secret.existingSecret can be used to reference an existing Kubernetes Secret.
      existingSecret: ""
  security:
    ## @skip global.security.allowInsecureImages
    allowInsecureImages: true
orchestration:
  ## @param orchestration.clusterSize number of Zeebe brokers
  clusterSize: "3"
  podDisruptionBudget:
    ## @param orchestration.podDisruptionBudget.enabled if true a pod disruption budget is defined
    enabled: false
`

func TestExtract_ParsesParamExtraAndSkip(t *testing.T) {
	fields, err := Extract([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	f, ok := fields["orchestration.clusterSize"]
	if !ok {
		t.Fatal("expected orchestration.clusterSize to be extracted")
	}
	if f.Tag != TagParam {
		t.Errorf("expected TagParam, got %s", f.Tag)
	}
	if f.Description != "number of Zeebe brokers" {
		t.Errorf("unexpected description: %q", f.Description)
	}
	if f.Default != "3" {
		t.Errorf("expected default \"3\", got %#v", f.Default)
	}
	if f.Kind != KindString {
		t.Errorf("expected KindString (clusterSize is a quoted numeric string in the real chart), got %s", f.Kind)
	}
	if !f.Resolved {
		t.Error("expected clusterSize to resolve")
	}

	pdb, ok := fields["orchestration.podDisruptionBudget.enabled"]
	if !ok {
		t.Fatal("expected orchestration.podDisruptionBudget.enabled to be extracted")
	}
	if pdb.Kind != KindBool || pdb.Default != false {
		t.Errorf("expected KindBool/false, got %s/%#v", pdb.Kind, pdb.Default)
	}

	skip, ok := fields["global.security.allowInsecureImages"]
	if !ok {
		t.Fatal("expected the @skip field to still be extracted (callers decide whether to surface it)")
	}
	if skip.Tag != TagSkip {
		t.Errorf("expected TagSkip, got %s", skip.Tag)
	}

	extra, ok := fields["global.license.secret"]
	if !ok {
		t.Fatal("expected the @extra container field to be extracted")
	}
	if extra.Tag != TagExtra {
		t.Errorf("expected TagExtra, got %s", extra.Tag)
	}
	if extra.Kind != KindMap {
		t.Errorf("expected KindMap for a container node, got %s", extra.Kind)
	}
}

func TestExtract_EmptyStringDefaultIsNullNotUnresolved(t *testing.T) {
	fields, err := Extract([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	f := fields["global.license.secret.inlineSecret"]
	if !f.Resolved {
		t.Error("an empty-string default should still count as resolved")
	}
	if f.Kind != KindNull {
		t.Errorf("expected KindNull for an empty string default, got %s", f.Kind)
	}
}

func TestUnresolved_FlagsMissingAndRenamedPaths(t *testing.T) {
	fields, err := Extract([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	missing := Unresolved(fields, []string{
		"orchestration.clusterSize",          // present, resolved — not missing
		"orchestration.doesNotExist",         // never annotated at all
		"global.license.secret.inlineSecret", // present but empty — not missing
	})
	if len(missing) != 1 || missing[0] != "orchestration.doesNotExist" {
		t.Errorf("expected exactly [orchestration.doesNotExist], got %v", missing)
	}
}

func TestExtract_RealChart(t *testing.T) {
	for _, chartPath := range []string{
		"/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.9/values.yaml",
		"/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.10/values.yaml",
	} {
		t.Run(chartPath, func(t *testing.T) {
			data, err := os.ReadFile(chartPath)
			if err != nil {
				t.Skipf("real chart checkout not available at %s: %v", chartPath, err)
			}
			fields, err := Extract(data)
			if err != nil {
				t.Fatalf("Extract failed on the real chart: %v", err)
			}
			if len(fields) < 100 {
				t.Errorf("expected at least 100 annotated fields from a real chart values.yaml, got %d — extraction is likely broken, not the chart being small", len(fields))
			}

			// Every unresolved @param is expected to be one of the documented
			// Spring Boot logging.level.* literal-dotted-key fields (see the package
			// doc comment) — anything else here is a genuine regression, either in
			// this package or in a chart change this package hasn't been taught about.
			var unexpected []string
			for path, f := range fields {
				if f.Tag != TagParam || f.Resolved {
					continue
				}
				if strings.Contains(path, "logging.level.io.") {
					continue
				}
				unexpected = append(unexpected, path)
			}
			if len(unexpected) > 0 {
				t.Errorf("unexpected unresolved @param fields (not a known logging.level.* literal-dotted-key case): %v", unexpected)
			}
		})
	}
}
