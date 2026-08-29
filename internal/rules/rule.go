// Package rules encodes the recurring Camunda 8 Self-Managed Helm chart misconfigurations
// documented in the "Blast Radius" operational audit as executable checks, so they can be
// caught before a customer hits them in production instead of after.
package rules

import "github.com/hamza-m-masood/camunda-chart-doctor/internal/values"

// Severity ranks how bad it is if this finding is real and left unaddressed.
type Severity string

const (
	Critical Severity = "CRITICAL"
	High     Severity = "HIGH"
	Medium   Severity = "MEDIUM"
	Low      Severity = "LOW"
)

var severityRank = map[Severity]int{Critical: 0, High: 1, Medium: 2, Low: 3}

// RankOf returns a lower-is-worse ordering for Severity, used for sorting and for
// --fail-on threshold comparisons.
func RankOf(s Severity) int { return severityRank[s] }

// Finding is one concrete, evidenced problem surfaced by a rule.
type Finding struct {
	RuleID      string   `json:"ruleId"`
	Severity    Severity `json:"severity"`
	Title       string   `json:"title"`
	Path        string   `json:"path,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
}

// ValuesCheck inspects the effective (merged) Helm values for one component of an
// installation and returns zero or more findings.
type ValuesCheck func(v values.Values) []Finding

// ManifestDoc is one rendered Kubernetes object from `helm template`, kept as both a
// lightweight kind/name identity and the raw YAML text for simple, dependency-free
// pattern checks against rendered output.
type ManifestDoc struct {
	Kind string
	Name string
	Text string
}

// ManifestCheck inspects the fully rendered manifests for an installation.
type ManifestCheck func(docs []ManifestDoc) []Finding

// AllValuesChecks returns every registered values-based rule.
func AllValuesChecks() []ValuesCheck {
	return []ValuesCheck{
		CheckPDBDisabled,
		CheckMemoryBurstable,
		CheckSecretKeyMissing,
		CheckDefaultCredentials,
		CheckRetentionDisabled,
		CheckReadinessTimeoutLow,
		CheckReplicationFactor,
		CheckImageDigestMissing,
		CheckServiceMonitorDisabled,
		CheckBundledPostgresBackupDisabled,
		CheckAntiAffinityNotReleaseScoped,
	}
}

// AllManifestChecks returns every registered manifest-based rule.
func AllManifestChecks() []ManifestCheck {
	return []ManifestCheck{
		CheckLegacyBitnamiImages,
		CheckPlaintextSecretsInConfigMap,
		CheckOrchestrationGracePeriodAndHeapDump,
	}
}

// Meta is static metadata about a rule, independent of any specific Finding it may
// or may not have produced in a given run — used to build a complete rules catalog
// (e.g. a SARIF report's tool.driver.rules array must list every rule the tool CAN
// report, not just the ones that fired this run).
type Meta struct {
	ID       string
	Title    string
	Severity Severity // the rule's worst-case severity, for rules whose actual
	// severity varies by where it fires (e.g. CCD002 is Critical on the
	// orchestration component, Medium elsewhere).
}

// AllRuleMetas returns static metadata for every rule this tool can produce a
// Finding for, values-based and manifest-based alike.
func AllRuleMetas() []Meta {
	return []Meta{
		{"CCD001", "orchestration.podDisruptionBudget is disabled", High},
		{"CCD002", "Memory requests below limits (Burstable QoS)", Critical},
		{"CCD003", "existingSecret is set without existingSecretKey", High},
		{"CCD004", "Default shipped credential is still active", Medium},
		{"CCD005", "Index/log retention (ILM) is disabled", Medium},
		{"CCD006", "readinessProbe.timeoutSeconds is too low", Low},
		{"CCD007", "replicationFactor exceeds clusterSize", High},
		{"CCD008", "Image pinned by tag only, no digest", Low},
		{"CCD009", "Rendered manifest references a frozen Bitnami legacy image", High},
		{"CCD010", "Rendered ConfigMap embeds a plaintext credential", High},
		{"CCD011", "PodDisruptionBudget object missing or ineffective (live)", High},
		{"CCD012", "Referenced Secret or key missing live (drift)", High},
		{"CCD013", "prometheusServiceMonitor.enabled is false", Medium},
		{"CCD014", "Bundled PostgreSQL enabled with backup disabled", Medium},
		{"CCD015", "Known chart limitation: grace period / heap dump path", Low},
		{"CCD016", "Default anti-affinity is not release-scoped", Low},
	}
}
