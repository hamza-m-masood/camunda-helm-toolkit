package monitorgen

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateServiceMonitors emits one ServiceMonitor per Service that has a
// recognized metrics port, targeting that Service's own selector and the exact
// port name found on it — never a hardcoded guess.
func GenerateServiceMonitors(release, namespace string, services []ServiceInfo) string {
	var b strings.Builder
	names := make([]ServiceInfo, len(services))
	copy(names, services)
	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })

	count := 0
	for _, s := range names {
		port, ok := PickMetricsPort(s)
		if !ok {
			continue
		}
		count++
		fmt.Fprintf(&b, "apiVersion: monitoring.coreos.com/v1\n")
		fmt.Fprintf(&b, "kind: ServiceMonitor\n")
		fmt.Fprintf(&b, "metadata:\n  name: %s\n  namespace: %s\n", s.Name, namespace)
		fmt.Fprintf(&b, "spec:\n")
		fmt.Fprintf(&b, "  selector:\n    matchLabels:\n")
		for k, v := range sortedSelector(s) {
			fmt.Fprintf(&b, "      %s: %q\n", k, v)
		}
		fmt.Fprintf(&b, "  endpoints:\n    - port: %s\n      path: /actuator/prometheus\n", port)
		fmt.Fprintf(&b, "---\n")
	}
	if count == 0 {
		return "# No rendered Service exposed a recognized metrics port " +
			"(metrics/management/http-management/server) — nothing generated.\n"
	}
	return b.String()
}

func sortedSelector(s ServiceInfo) map[string]string {
	sel := s.Selector
	if len(sel) == 0 {
		sel = s.Labels
	}
	return sel
}

// GeneratePrometheusRule emits one baseline PrometheusRule covering the failure
// modes this tool's own --live checks already look for, so an operator gets an
// alert BEFORE the next `check --live` run would have to tell them about it.
//
// The exporter/backpressure alert is deliberately left commented out: this
// package could not verify the exact Zeebe metric name for exporter lag or
// backpressure with confidence in this environment, and presenting an unverified
// metric name as fact would be worse than omitting it (see the audit finding this
// tool is built to avoid repeating). Uncomment and adjust it only after confirming
// the metric name against your own Zeebe version's /actuator/prometheus output.
func GeneratePrometheusRule(release, namespace string) string {
	restartExpr := fmt.Sprintf(
		`increase(kube_pod_container_status_restarts_total{namespace="%s", pod=~"%s-zeebe-.*"}[15m]) > 0`,
		namespace, release)
	pdbExpr := fmt.Sprintf(
		`kube_poddisruptionbudget_status_disruptions_allowed{namespace="%s", poddisruptionbudget=~"%s-zeebe.*"} == 0`,
		namespace, release)

	return fmt.Sprintf(`apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: %s-camunda-helm-toolkit-baseline
  namespace: %s
spec:
  groups:
    - name: camunda-helm-toolkit-baseline
      rules:
        - alert: ZeebeBrokerRestarting
          expr: %s
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "A Zeebe broker pod restarted in the last 15 minutes"
            description: >-
              Requires kube-state-metrics. See CCD002 (Burstable QoS / memory limits)
              and CCD015 (heap dump path) for the most common causes.
        - alert: ZeebePDBBlocked
          expr: %s
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: "The Zeebe broker PodDisruptionBudget currently allows zero disruptions"
            description: >-
              Requires kube-state-metrics. Matches this tool's own CCD011 live check —
              can be transient (a broker is down) or a permanent minAvailable
              misconfiguration; investigate before assuming it is protective.
        # - alert: ZeebeExporterLagging
        #   expr: <VERIFY THIS METRIC NAME against your Zeebe version's
        #     /actuator/prometheus output before enabling — this tool could not
        #     confirm an exact exporter-lag metric name with confidence and will
        #     not present a guess as fact>
        #   for: 15m
        #   labels:
        #     severity: warning
        #   annotations:
        #     summary: "Zeebe exporter appears to be falling behind"
`, release, namespace, restartExpr, pdbExpr)
}
