package helmrender_test

import (
	"strings"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/helmrender"
)

// TestTemplate_UsesTheGivenReleaseName_NotAPlaceholder guards against the exact bug an
// adversarial audit found: scaffold-monitoring generated release-scoped selectors
// carrying a hardcoded placeholder release name instead of the one the operator asked
// for, because Template silently ignored it. Any caller depending on
// app.kubernetes.io/instance (or, as here, {{ .Release.Name }}) in rendered output must
// see its own release name reflected, not some other string.
func TestTemplate_UsesTheGivenReleaseName_NotAPlaceholder(t *testing.T) {
	const releaseName = "a-totally-distinctive-release-name"
	raw, err := helmrender.Template("../../testdata/fakechart", nil, releaseName)
	if err != nil {
		t.Fatalf("Template failed: %v (is `helm` on PATH?)", err)
	}
	out := string(raw)
	if !strings.Contains(out, releaseName+"-fakechart-dummy") {
		t.Errorf("expected rendered output to contain %q, got:\n%s", releaseName+"-fakechart-dummy", out)
	}
	if strings.Contains(out, "camunda-helm-toolkit-fakechart-dummy") {
		t.Errorf("rendered output still contains the old hardcoded placeholder name, release name was not honored:\n%s", out)
	}
}
