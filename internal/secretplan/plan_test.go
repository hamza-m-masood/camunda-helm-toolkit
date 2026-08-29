package secretplan_test

import (
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/secretplan"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func TestPlan_WebModelerPusherSecret_ProducesValidNestedOverlay(t *testing.T) {
	secrets := []live.SecretSummary{
		{Name: "my-release-web-modeler", Type: "Opaque", Keys: []string{"pusher-app-secret", "pusher-app-key"}},
	}
	effective, err := values.ParseYAML([]byte(`webModeler: {enabled: true}`))
	if err != nil {
		t.Fatal(err)
	}

	recs := secretplan.Plan(effective, secrets, "my-namespace")
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Confidence != secretplan.Known {
		t.Fatalf("expected Known confidence, got %s", rec.Confidence)
	}
	if rec.OverlayYAML == "" {
		t.Fatal("expected a non-empty overlay")
	}

	// The whole point: the overlay must be VALID, correctly nested YAML that
	// resolves to the real chart's actual values paths — not flattened dotted keys.
	parsed, err := values.ParseYAML([]byte(rec.OverlayYAML))
	if err != nil {
		t.Fatalf("overlay did not parse as YAML: %v\n---\n%s", err, rec.OverlayYAML)
	}
	// Critical: the overlay must NOT reference the secret's own original name.
	// Verified end-to-end on a real cluster that doing so makes the chart stop
	// rendering that Secret and helm upgrade prunes it — the opposite of "survives".
	const pinnedName = "my-release-web-modeler-pinned"
	es, ok := values.GetPath(parsed, "webModeler.restapi.pusher.secret.existingSecret")
	if !ok || es != pinnedName {
		t.Errorf("webModeler.restapi.pusher.secret.existingSecret = %v (ok=%v), want %s", es, ok, pinnedName)
	}
	esk, ok := values.GetPath(parsed, "webModeler.restapi.pusher.secret.existingSecretKey")
	if !ok || esk != "pusher-app-secret" {
		t.Errorf("webModeler.restapi.pusher.secret.existingSecretKey = %v (ok=%v), want pusher-app-secret", esk, ok)
	}
	es2, ok := values.GetPath(parsed, "webModeler.restapi.pusher.client.secret.existingSecret")
	if !ok || es2 != pinnedName {
		t.Errorf("webModeler.restapi.pusher.client.secret.existingSecret = %v (ok=%v), want %s", es2, ok, pinnedName)
	}
	esk2, ok := values.GetPath(parsed, "webModeler.restapi.pusher.client.secret.existingSecretKey")
	if !ok || esk2 != "pusher-app-key" {
		t.Errorf("webModeler.restapi.pusher.client.secret.existingSecretKey = %v (ok=%v)", esk2, ok)
	}

	if len(rec.BackupCommands) != 1 || !strings.Contains(rec.BackupCommands[0], "my-release-web-modeler") {
		t.Errorf("expected a backup command naming the secret, got %+v", rec.BackupCommands)
	}
	if len(rec.CreateCommands) != 1 ||
		!strings.Contains(rec.CreateCommands[0], "my-release-web-modeler") ||
		!strings.Contains(rec.CreateCommands[0], pinnedName) {
		t.Errorf("expected a create-command copying the original secret to the pinned name, got %+v", rec.CreateCommands)
	}
}

func TestPlan_AlreadyPinned_ProducesNoRecommendation(t *testing.T) {
	secrets := []live.SecretSummary{
		{Name: "my-release-web-modeler", Type: "Opaque", Keys: []string{"pusher-app-secret", "pusher-app-key"}},
	}
	effective, err := values.ParseYAML([]byte(`
webModeler:
  restapi:
    pusher:
      secret:
        existingSecret: my-release-web-modeler
        existingSecretKey: pusher-app-secret
      client:
        secret:
          existingSecret: my-release-web-modeler
          existingSecretKey: pusher-app-key
`))
	if err != nil {
		t.Fatal(err)
	}
	recs := secretplan.Plan(effective, secrets, "ns")
	if len(recs) != 0 {
		t.Fatalf("expected 0 recommendations once already pinned, got %d: %+v", len(recs), recs)
	}
}

func TestPlan_UnmappedSecret_ProducesAdvisoryOnly(t *testing.T) {
	secrets := []live.SecretSummary{
		{Name: "my-release-some-other-secret", Type: "Opaque", Keys: []string{"password"}},
	}
	effective, _ := values.ParseYAML([]byte(`foo: bar`))
	recs := secretplan.Plan(effective, secrets, "ns")
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	if recs[0].Confidence != secretplan.Unmapped {
		t.Errorf("expected Unmapped confidence, got %s", recs[0].Confidence)
	}
	if recs[0].OverlayYAML != "" {
		t.Errorf("expected no overlay for an unmapped secret, got %q", recs[0].OverlayYAML)
	}
}

func TestPlan_AlreadyReferencedByNameGenerically_ProducesNoRecommendation(t *testing.T) {
	secrets := []live.SecretSummary{
		{Name: "my-custom-secret", Type: "Opaque", Keys: []string{"password"}},
	}
	effective, err := values.ParseYAML([]byte(`
global:
  elasticsearch:
    auth:
      secret:
        existingSecret: my-custom-secret
        existingSecretKey: password
`))
	if err != nil {
		t.Fatal(err)
	}
	recs := secretplan.Plan(effective, secrets, "ns")
	if len(recs) != 0 {
		t.Fatalf("expected 0 recommendations for a secret already referenced by name, got %d: %+v", len(recs), recs)
	}
}
