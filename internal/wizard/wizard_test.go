package wizard

import (
	"os/exec"
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

// syntheticChartDefaults mirrors the real chart's shape just enough to exercise every
// fix Build makes, without this test depending on the real chart's exact current
// numbers (which will drift over time) -- deliberately Burstable everywhere, like the
// real chart, and missing every other fix Build is responsible for applying.
const syntheticChartDefaults = `
orchestration:
  resources:
    requests: {memory: "1500Mi"}
    limits: {memory: "3000Mi"}
  podDisruptionBudget:
    enabled: false
  retention:
    enabled: false
  history:
    retention:
      enabled: false
  readinessProbe:
    timeoutSeconds: 1
  replicationFactor: "3"
  clusterSize: "3"
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/component
                operator: In
                values: [zeebe-broker]
          topologyKey: kubernetes.io/hostname
  security:
    initialization:
      users:
        - username: demo
          password: demo

identity:
  enabled: true
  resources:
    requests: {memory: "400Mi"}
    limits: {memory: "2Gi"}

connectors:
  enabled: true
  resources:
    requests: {memory: "1Gi"}
    limits: {memory: "2Gi"}

optimize:
  enabled: false
  resources:
    requests: {memory: "1Gi"}
    limits: {memory: "2Gi"}

webModeler:
  enabled: false
  restapi:
    resources:
      requests: {memory: "1280Mi"}
      limits: {memory: "2560Mi"}
  websockets:
    resources:
      requests: {memory: "64Mi"}
      limits: {memory: "128Mi"}

identityKeycloak:
  enabled: false
  resources:
    requests: {memory: "1Gi"}
    limits: {memory: "2Gi"}

prometheusServiceMonitor:
  enabled: false
`

func baseAnswers() Answers {
	return Answers{
		ReleaseName:          "camunda",
		SecondaryStorageType: "elasticsearch",
		AuthMethod:           "basic",
		AdminUsername:        "admin",
		AdminPassword:        "a-real-password-not-demo",
	}
}

func runValuesChecks(t *testing.T, v values.Values) []rules.Finding {
	t.Helper()
	var findings []rules.Finding
	for _, check := range rules.AllValuesChecks() {
		findings = append(findings, check(v)...)
	}
	return findings
}

// TestBuild_NeverTripsItsOwnRules is this package's equivalent of capacityplan's
// TestRecommend_NeverTripsItsOwnRules: init's entire reason to exist is that its output
// already passes `check`, so this must hold across a real matrix of answers, not just
// one happy path. CCD008 (digest pinning) is the one documented, deliberate exception --
// see the package doc comment and the note Build appends about it.
func TestBuild_NeverTripsItsOwnRules(t *testing.T) {
	chartDefaults, err := values.ParseYAML([]byte(syntheticChartDefaults))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		a    Answers
	}{
		{"minimal, everything else off", baseAnswers()},
		{"every optional component on", func() Answers {
			a := baseAnswers()
			a.EnableIdentity, a.EnableConnectors, a.EnableOptimize, a.EnableWebModeler = true, true, true, true
			a.WebModelerFromEmail = "camunda@example.invalid"
			return a
		}()},
		{"oidc auth", func() Answers {
			a := baseAnswers()
			a.AuthMethod = "oidc"
			a.AdminUsername, a.AdminPassword = "", ""
			a.OIDCIssuerURL = "https://issuer.example.invalid"
			return a
		}()},
		{"opensearch, ingress with tls", func() Answers {
			a := baseAnswers()
			a.SecondaryStorageType = "opensearch"
			a.IngressHost = "camunda.example.invalid"
			a.IngressTLS = true
			return a
		}()},
		{"rdbms, no sizing answered", func() Answers {
			a := baseAnswers()
			a.SecondaryStorageType = "rdbms"
			return a
		}()},
		{"low throughput", func() Answers {
			a := baseAnswers()
			a.ThroughputPerSec, a.AvgPayloadKB = 1, 1
			return a
		}()},
		{"high throughput", func() Answers {
			a := baseAnswers()
			a.ThroughputPerSec, a.AvgPayloadKB, a.RetentionDays = 5000, 32, 90
			a.EnableIdentity, a.EnableConnectors, a.EnableOptimize, a.EnableWebModeler = true, true, true, true
			a.WebModelerFromEmail = "camunda@example.invalid"
			return a
		}()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, notes, err := Build(c.a, chartDefaults)
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}
			if len(notes) == 0 {
				t.Error("expected at least the standing digest/plan-secrets notes")
			}

			merged := values.DeepMerge(chartDefaults, out)
			findings := runValuesChecks(t, merged)

			var unexpected []rules.Finding
			for _, f := range findings {
				if IsDocumentedException(f) {
					continue
				}
				unexpected = append(unexpected, f)
			}
			if len(unexpected) > 0 {
				for _, f := range unexpected {
					t.Errorf("unexpected finding %s at %s: %s", f.RuleID, f.Path, f.Title)
				}
			}
		})
	}
}

func TestBuild_ValidatesRequiredAnswers(t *testing.T) {
	chartDefaults, _ := values.ParseYAML([]byte(syntheticChartDefaults))

	cases := []struct {
		name string
		a    Answers
	}{
		{"missing release name", func() Answers { a := baseAnswers(); a.ReleaseName = ""; return a }()},
		{"missing secondary storage", func() Answers { a := baseAnswers(); a.SecondaryStorageType = ""; return a }()},
		{"basic auth missing password", func() Answers { a := baseAnswers(); a.AdminPassword = ""; return a }()},
		{"oidc missing issuer", func() Answers {
			a := baseAnswers()
			a.AuthMethod = "oidc"
			return a
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := Build(c.a, chartDefaults); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

func TestBuild_ReleaseScopesAntiAffinity(t *testing.T) {
	chartDefaults, _ := values.ParseYAML([]byte(syntheticChartDefaults))
	out, _, err := Build(baseAnswers(), chartDefaults)
	if err != nil {
		t.Fatal(err)
	}
	terms, ok := values.GetPath(out, "orchestration.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution")
	if !ok {
		t.Fatal("expected an anti-affinity override")
	}
	list := terms.([]interface{})
	term := list[0].(map[string]interface{})
	exprs := term["labelSelector"].(map[string]interface{})["matchExpressions"].([]interface{})
	foundInstance := false
	for _, e := range exprs {
		em := e.(map[string]interface{})
		if em["key"] == "app.kubernetes.io/instance" {
			foundInstance = true
			vals := em["values"].([]interface{})
			if vals[0] != "camunda" {
				t.Errorf("expected the release name baked in, got %v", vals)
			}
		}
	}
	if !foundInstance {
		t.Error("expected an app.kubernetes.io/instance matchExpression")
	}
}

func TestBuild_NeverTripsItsOwnRules_RealChart(t *testing.T) {
	for _, chartPath := range []string{
		"/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.9",
		"/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.10",
	} {
		t.Run(chartPath, func(t *testing.T) {
			out, err := exec.Command("helm", "show", "values", chartPath).Output()
			if err != nil {
				t.Skipf("real chart checkout not usable at %s: %v", chartPath, err)
			}
			chartDefaults, err := values.ParseYAML(out)
			if err != nil {
				t.Fatalf("parsing real chart values.yaml: %v", err)
			}

			a := baseAnswers()
			a.EnableIdentity, a.EnableConnectors, a.EnableOptimize, a.EnableWebModeler = true, true, true, true
			a.WebModelerFromEmail = "camunda@example.invalid"
			a.ThroughputPerSec, a.AvgPayloadKB = 800, 2

			generated, _, err := Build(a, chartDefaults)
			if err != nil {
				t.Fatalf("Build failed against the real chart: %v", err)
			}
			merged := values.DeepMerge(chartDefaults, generated)
			var unexpected []rules.Finding
			for _, f := range runValuesChecks(t, merged) {
				if IsDocumentedException(f) {
					continue
				}
				unexpected = append(unexpected, f)
			}
			for _, f := range unexpected {
				t.Errorf("unexpected finding %s at %s: %s", f.RuleID, f.Path, f.Title)
			}
		})
	}
}
