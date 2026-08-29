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
	}
}

// AllManifestChecks returns every registered manifest-based rule.
func AllManifestChecks() []ManifestCheck {
	return []ManifestCheck{
		CheckLegacyBitnamiImages,
		CheckPlaintextSecretsInConfigMap,
	}
}
