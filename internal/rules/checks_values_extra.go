package rules

import (
	"fmt"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

// CheckServiceMonitorDisabled: prometheusServiceMonitor.enabled defaults to false.
// With it off, a Prometheus Operator already running in-cluster still gets zero
// scrape targets from this chart — none of the other findings in this tool (or any
// alert an operator might build) are observable until this is turned on.
func CheckServiceMonitorDisabled(v values.Values) []Finding {
	val, _ := values.GetPath(v, "prometheusServiceMonitor.enabled")
	enabled, _ := val.(bool)
	if enabled {
		return nil
	}
	return []Finding{{
		RuleID:   "CCD013",
		Severity: Medium,
		Title:    "prometheusServiceMonitor.enabled is false",
		Path:     "prometheusServiceMonitor.enabled",
		Detail: "No ServiceMonitor is rendered for any component. Even with the Prometheus " +
			"Operator already running in-cluster, this chart produces zero scrape targets — " +
			"every other reliability problem (backpressure, OOM restarts, PDB coverage) is " +
			"invisible to monitoring until this is enabled.",
		Remediation: "Set prometheusServiceMonitor.enabled: true if a Prometheus Operator is present in-cluster.",
	}}
}

// CheckBundledPostgresBackupDisabled flags the bundled Identity/Web Modeler
// PostgreSQL subcharts when they are actually enabled but their own backup is left
// at its default off — a logical-dump-only mechanism the chart never turns on by
// itself, so an enabled bundled Postgres with no backup is a straightforward miss,
// not a deliberate choice most operators would make.
func CheckBundledPostgresBackupDisabled(v values.Values) []Finding {
	checks := []struct{ prefix, label string }{
		{"identityPostgresql", "identityPostgresql"},
		{"webModelerPostgresql", "webModelerPostgresql"},
	}
	var findings []Finding
	for _, c := range checks {
		enabledVal, _ := values.GetPath(v, c.prefix+".enabled")
		enabled, _ := enabledVal.(bool)
		if !enabled {
			continue
		}
		backupVal, _ := values.GetPath(v, c.prefix+".backup.enabled")
		backupEnabled, _ := backupVal.(bool)
		if backupEnabled {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "CCD014",
			Severity: Medium,
			Title:    fmt.Sprintf("%s is enabled with backup disabled", c.label),
			Path:     c.prefix + ".backup.enabled",
			Detail: fmt.Sprintf(
				"%s.enabled is true, but %s.backup (a scheduled logical-dump cronjob — not "+
					"point-in-time recovery, just the chart's only built-in backup mechanism) is "+
					"off. A PVC loss or corruption on this bundled database loses all its data with "+
					"no recovery path.", c.label, c.label),
			Remediation: fmt.Sprintf(
				"Set %s.backup.enabled: true, or migrate to an externally managed PostgreSQL "+
					"instance with its own backup/PITR story.", c.label),
		})
	}
	return findings
}

// CheckAntiAffinityNotReleaseScoped flags the chart's shipped default broker
// anti-affinity, which matches only on app.kubernetes.io/component with no
// app.kubernetes.io/instance term. This is informational about a real chart
// limitation with two concrete consequences: a second release in the same
// namespace can never schedule its brokers (every node already "has one"), and a
// node drain during a single release's lifetime has nowhere else to place an
// evicted broker on a minimally-sized cluster.
func CheckAntiAffinityNotReleaseScoped(v values.Values) []Finding {
	val, ok := values.GetPath(v, "orchestration.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution")
	if !ok {
		return nil
	}
	terms, ok := val.([]interface{})
	if !ok {
		return nil
	}
	for _, t := range terms {
		term, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		sel, ok := term["labelSelector"].(map[string]interface{})
		if !ok {
			continue
		}
		exprs, ok := sel["matchExpressions"].([]interface{})
		if !ok {
			continue
		}
		hasComponent, hasInstance := false, false
		for _, e := range exprs {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			switch em["key"] {
			case "app.kubernetes.io/component":
				hasComponent = true
			case "app.kubernetes.io/instance":
				hasInstance = true
			}
		}
		if hasComponent && !hasInstance {
			return []Finding{{
				RuleID:   "CCD016",
				Severity: Low,
				Title:    "orchestration.affinity still matches the chart's shipped (non-release-scoped) default",
				Path:     "orchestration.affinity.podAntiAffinity",
				Detail: "The anti-affinity rule matches on app.kubernetes.io/component alone, with " +
					"no app.kubernetes.io/instance term. Two consequences: a second Camunda release " +
					"in the same namespace can never schedule its brokers (every node already hosts " +
					"one from the first release), and on a minimally-sized node pool a drain has " +
					"nowhere else to place an evicted broker, since every other node is already " +
					"excluded by this same rule.",
				Remediation: "Add app.kubernetes.io/instance to the matchExpressions so the rule is " +
					"release-scoped, or accept the tradeoff explicitly if you only ever run one release per namespace.",
			}}
		}
	}
	return nil
}
