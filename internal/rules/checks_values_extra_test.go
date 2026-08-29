package rules_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

func mustParse(t *testing.T, yamlText string) values.Values {
	t.Helper()
	v, err := values.ParseYAML([]byte(yamlText))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCheckServiceMonitorDisabled(t *testing.T) {
	off := mustParse(t, `prometheusServiceMonitor: {enabled: false}`)
	if f := rules.CheckServiceMonitorDisabled(off); len(f) != 1 {
		t.Errorf("expected 1 finding when disabled, got %d", len(f))
	}
	on := mustParse(t, `prometheusServiceMonitor: {enabled: true}`)
	if f := rules.CheckServiceMonitorDisabled(on); len(f) != 0 {
		t.Errorf("expected 0 findings when enabled, got %d", len(f))
	}
	absent := mustParse(t, `other: {}`)
	if f := rules.CheckServiceMonitorDisabled(absent); len(f) != 1 {
		t.Errorf("expected 1 finding when key absent (treated as disabled), got %d", len(f))
	}
}

func TestCheckBundledPostgresBackupDisabled(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int
	}{
		{"both disabled component -> no finding", `identityPostgresql: {enabled: false}`, 0},
		{"enabled, backup off -> finding", `identityPostgresql: {enabled: true, backup: {enabled: false}}`, 1},
		{"enabled, backup absent -> finding", `identityPostgresql: {enabled: true}`, 1},
		{"enabled, backup on -> no finding", `identityPostgresql: {enabled: true, backup: {enabled: true}}`, 0},
		{
			"both subcharts enabled with backup off -> 2 findings",
			`identityPostgresql: {enabled: true}
webModelerPostgresql: {enabled: true}`,
			2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := mustParse(t, c.yaml)
			got := rules.CheckBundledPostgresBackupDisabled(v)
			if len(got) != c.want {
				t.Errorf("got %d findings, want %d: %+v", len(got), c.want, got)
			}
		})
	}
}

func TestCheckAntiAffinityNotReleaseScoped(t *testing.T) {
	defaultShape := mustParse(t, `
orchestration:
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/component
                operator: In
                values: [zeebe-broker]
          topologyKey: kubernetes.io/hostname
`)
	if f := rules.CheckAntiAffinityNotReleaseScoped(defaultShape); len(f) != 1 {
		t.Errorf("expected 1 finding for the chart's default shape, got %d", len(f))
	}

	scoped := mustParse(t, `
orchestration:
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/component
                operator: In
                values: [zeebe-broker]
              - key: app.kubernetes.io/instance
                operator: In
                values: [my-release]
          topologyKey: kubernetes.io/hostname
`)
	if f := rules.CheckAntiAffinityNotReleaseScoped(scoped); len(f) != 0 {
		t.Errorf("expected 0 findings once instance-scoped, got %d", len(f))
	}

	absent := mustParse(t, `orchestration: {}`)
	if f := rules.CheckAntiAffinityNotReleaseScoped(absent); len(f) != 0 {
		t.Errorf("expected 0 findings when affinity block is absent entirely, got %d", len(f))
	}
}
