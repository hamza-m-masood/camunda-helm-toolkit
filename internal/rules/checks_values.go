package rules

import (
	"fmt"
	"strings"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

// CheckPDBDisabled: orchestration.podDisruptionBudget.enabled defaults to false. On the
// default 3-broker / replicationFactor-3 cluster, a routine node drain or autoscaler
// consolidation can evict two of three brokers at once and lose Raft quorum.
func CheckPDBDisabled(v values.Values) []Finding {
	val, _ := values.GetPath(v, "orchestration.podDisruptionBudget.enabled")
	enabled, _ := val.(bool)
	if enabled {
		return nil
	}
	return []Finding{{
		RuleID:   "CCD001",
		Severity: High,
		Title:    "orchestration.podDisruptionBudget is disabled",
		Path:     "orchestration.podDisruptionBudget.enabled",
		Detail: "No PodDisruptionBudget protects the Zeebe broker StatefulSet. A node drain, " +
			"cluster-autoscaler consolidation, or managed node-pool upgrade can evict a quorum " +
			"of brokers simultaneously, halting every partition until they reschedule and catch up.",
		Remediation: "Set orchestration.podDisruptionBudget.enabled: true " +
			"(maxUnavailable: 1 is safe for replicationFactor >= 3).",
	}}
}

// CheckMemoryBurstable finds every resources block anywhere in the values tree where
// requests.memory is set below limits.memory. This is a schema-agnostic scan (not
// scoped to a component list) because the audit found the pattern universal across
// every component, not just the Zeebe broker — though the Zeebe broker's failure mode
// (lost quorum, corrupted Raft journal) is more severe than a stateless pod restart.
func CheckMemoryBurstable(v values.Values) []Finding {
	var findings []Finding
	for _, b := range values.FindBlocks(v, "requests", "limits") {
		req, ok1 := b.Node["requests"].(map[string]interface{})
		lim, ok2 := b.Node["limits"].(map[string]interface{})
		if !ok1 || !ok2 {
			continue
		}
		reqMemStr, ok3 := req["memory"].(string)
		limMemStr, ok4 := lim["memory"].(string)
		if !ok3 || !ok4 {
			continue
		}
		reqMem, ok5 := values.ParseQuantity(reqMemStr)
		limMem, ok6 := values.ParseQuantity(limMemStr)
		if !ok5 || !ok6 || limMem == 0 {
			continue
		}
		ratio := reqMem / limMem
		if ratio >= 0.999 {
			continue
		}
		sev := Medium
		if strings.HasPrefix(b.Path, "orchestration.") {
			sev = Critical
		}
		findings = append(findings, Finding{
			RuleID:   "CCD002",
			Severity: sev,
			Title:    fmt.Sprintf("%s: memory requests (%s) below limits (%s) — Burstable QoS", b.Path, reqMemStr, limMemStr),
			Path:     b.Path,
			Detail: fmt.Sprintf(
				"requests.memory is %.0f%% of limits.memory. This pod runs as Burstable QoS: it is "+
					"evicted ahead of Guaranteed pods under node memory pressure, and any usage spike "+
					"above the limit is an immediate, unwarned OOMKill.", ratio*100),
			Remediation: "Set requests.memory equal to limits.memory for Guaranteed QoS, " +
				"or confirm Burstable is a deliberate choice for this component.",
		})
	}
	return findings
}

// CheckSecretKeyMissing finds every "existingSecret" reference in the values tree whose
// sibling "existingSecretKey" is empty or absent. The chart's own normalizeSecretConfiguration
// helper requires both fields to be set together — when only existingSecret is set, the
// reference is silently dropped with no warning, and the component falls back to an
// empty-string credential. Auth then fails at the datastore, not at `helm install`.
func CheckSecretKeyMissing(v values.Values) []Finding {
	var findings []Finding
	for _, b := range values.FindBlocks(v, "existingSecret") {
		es, _ := b.Node["existingSecret"].(string)
		if strings.TrimSpace(es) == "" {
			continue
		}
		esk, _ := b.Node["existingSecretKey"].(string)
		if strings.TrimSpace(esk) != "" {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "CCD003",
			Severity: High,
			Title:    fmt.Sprintf("%s.existingSecret is set without existingSecretKey", b.Path),
			Path:     b.Path,
			Detail: fmt.Sprintf(
				"existingSecret=%q is configured but existingSecretKey is empty. The chart silently "+
					"drops this reference — the component falls back to an empty-string credential, so "+
					"auth fails at the datastore instead of at install time.", es),
			Remediation: "Set the matching existingSecretKey alongside existingSecret.",
		})
	}
	return findings
}

// CheckDefaultCredentials flags the chart's shipped example users (demo/demo,
// connectors/connector) if they are still present under orchestration's initial-user
// list — with global.security.authentication.method: basic (the chart default), these
// are working, documented, internet-reachable-if-exposed credentials.
func CheckDefaultCredentials(v values.Values) []Finding {
	usersRaw, ok := values.GetPath(v, "orchestration.security.initialization.users")
	if !ok {
		return nil
	}
	users, ok := usersRaw.([]interface{})
	if !ok {
		return nil
	}
	var findings []Finding
	for _, u := range users {
		um, ok := u.(map[string]interface{})
		if !ok {
			continue
		}
		user, _ := um["username"].(string)
		pass, _ := um["password"].(string)
		isDefault := (user == "demo" && pass == "demo") || (user == "connectors" && pass == "connector")
		if !isDefault {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "CCD004",
			Severity: Medium,
			Title:    fmt.Sprintf("Default credential %s/%s is still active", user, pass),
			Path:     "orchestration.security.initialization.users",
			Detail: "This is the chart's shipped example user, unmodified. If authentication.method " +
				"is basic (the chart default) and this is reachable, it is a working credential.",
			Remediation: "Override orchestration.security.initialization.users with real " +
				"credentials before any non-local deployment.",
		})
	}
	return findings
}

// CheckRetentionDisabled: orchestration.retention.enabled and
// orchestration.history.retention.enabled both default to false. Without an ILM policy,
// exported/archived indices grow unbounded until the secondary storage disk fills — at
// which point Elasticsearch/OpenSearch marks itself read-only and the whole engine
// backpressures. This is the highest-volume theme in the audit's incident correlation.
func CheckRetentionDisabled(v values.Values) []Finding {
	checks := []struct{ path, label string }{
		{"orchestration.retention.enabled", "orchestration.retention"},
		{"orchestration.history.retention.enabled", "orchestration.history.retention"},
	}
	var findings []Finding
	for _, c := range checks {
		val, ok := values.GetPath(v, c.path)
		enabled, isBool := val.(bool)
		if ok && isBool && enabled {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "CCD005",
			Severity: Medium,
			Title:    fmt.Sprintf("%s is disabled", c.label),
			Path:     c.path,
			Detail: "Without an ILM policy, exported/archived indices grow unbounded until the " +
				"secondary storage disk fills. At flood-stage watermark, Elasticsearch/OpenSearch " +
				"marks itself read-only and the whole engine backpressures.",
			Remediation: "Enable retention with a period matched to your compliance/query needs, " +
				"or confirm an externally managed ILM policy covers these indices.",
		})
	}
	return findings
}

// CheckReadinessTimeoutLow: a 1-second timeout on the actuator health endpoint is inside
// normal JVM GC-pause range. If exceeded cluster-wide during a load spike or CPU
// throttling, the client-facing gRPC Service can briefly lose all endpoints while the
// brokers are alive and processing.
func CheckReadinessTimeoutLow(v values.Values) []Finding {
	val, ok := values.GetPath(v, "orchestration.readinessProbe.timeoutSeconds")
	if !ok {
		return nil
	}
	t, err := values.ToInt(val)
	if err != nil || t > 1 {
		return nil
	}
	return []Finding{{
		RuleID:   "CCD006",
		Severity: Low,
		Title:    fmt.Sprintf("orchestration.readinessProbe.timeoutSeconds is %d", t),
		Path:     "orchestration.readinessProbe.timeoutSeconds",
		Detail: "A 1-second timeout is inside normal JVM GC-pause range. If exceeded, brokers are " +
			"marked NotReady and removed from the gRPC-serving Service — a cluster-wide readiness " +
			"flap can empty the Service's endpoint list while brokers are alive and processing.",
		Remediation: "Raise timeoutSeconds to 5 or more; periodSeconds already gives ample margin.",
	}}
}

// CheckReplicationFactor: nothing in the chart validates replicationFactor <= clusterSize.
// A cluster scaled down without also lowering replicationFactor CrashLoopBackOffs on a
// config-validation error at broker startup, while `helm upgrade` reports success.
func CheckReplicationFactor(v values.Values) []Finding {
	rfRaw, ok1 := values.GetPath(v, "orchestration.replicationFactor")
	csRaw, ok2 := values.GetPath(v, "orchestration.clusterSize")
	if !ok1 || !ok2 {
		return nil
	}
	rf, err1 := values.ToInt(rfRaw)
	cs, err2 := values.ToInt(csRaw)
	if err1 != nil || err2 != nil || rf <= cs {
		return nil
	}
	return []Finding{{
		RuleID:      "CCD007",
		Severity:    High,
		Title:       fmt.Sprintf("replicationFactor (%d) exceeds clusterSize (%d)", rf, cs),
		Path:        "orchestration.replicationFactor",
		Detail:      "Every broker will crash-loop at startup on a config-validation error. helm install/upgrade reports success regardless.",
		Remediation: "Set orchestration.replicationFactor <= orchestration.clusterSize.",
	}}
}

// CheckImageDigestMissing finds every image block (repository+tag) anywhere in the
// values tree with no digest pinned. A tag alone can silently point at a different
// image over time, and every pod restart re-resolves it against the registry.
func CheckImageDigestMissing(v values.Values) []Finding {
	var findings []Finding
	for _, b := range values.FindBlocks(v, "repository", "tag") {
		tag, _ := b.Node["tag"].(string)
		if strings.TrimSpace(tag) == "" {
			continue
		}
		digest, _ := b.Node["digest"].(string)
		if strings.TrimSpace(digest) != "" {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "CCD008",
			Severity: Low,
			Title:    fmt.Sprintf("%s pins by tag only, no digest", b.Path),
			Path:     b.Path,
			Detail: fmt.Sprintf(
				"tag=%q with an empty digest means the same tag can point at a different image over "+
					"time, and every pod restart re-resolves it against the registry.", tag),
			Remediation: "Set the matching .digest field — see the chart's values-digest.yaml for the digests shipped with this release.",
		})
	}
	return findings
}
