package rules

import (
	"fmt"
	"regexp"
	"strings"
)

var bitnamiLegacyRe = regexp.MustCompile(`bitnamilegacy/[a-zA-Z0-9._-]+`)

// CheckLegacyBitnamiImages scans rendered manifests for bitnamilegacy/* image
// references — Bitnami's archived, no-longer-patched registry. Unlike scanning raw
// values (which can't distinguish "set but unused because the component is disabled"
// from "actually rendered"), this checks what Helm actually produced.
func CheckLegacyBitnamiImages(docs []ManifestDoc) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, d := range docs {
		for _, m := range bitnamiLegacyRe.FindAllString(d.Text, -1) {
			key := d.Kind + "/" + d.Name + "/" + m
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, Finding{
				RuleID:   "CCD009",
				Severity: High,
				Title:    fmt.Sprintf("%s/%s references a frozen Bitnami legacy image (%s)", d.Kind, d.Name, m),
				Path:     d.Kind + "/" + d.Name,
				Detail: "bitnamilegacy/* is Bitnami's archived, no-longer-patched registry. " +
					"A component actually rendered against it receives no further security patches.",
				Remediation: "Point this component at a maintained image, or migrate to an " +
					"externally managed equivalent (see the chart's Bitnami-removal migration guidance).",
			})
		}
	}
	return findings
}

// suspiciousKeyRe matches a YAML "key: value" line whose key name is credential-shaped
// and whose value is a literal (not a template placeholder or env-var interpolation).
var suspiciousKeyRe = regexp.MustCompile(`(?i)^\s*(password|client-secret|secret)\s*:\s*"?([^"\s]{2,})"?\s*$`)

// CheckPlaintextSecretsInConfigMap scans rendered ConfigMap documents for lines that
// look like a literal credential rather than a templated placeholder — ConfigMaps are
// commonly readable by broader RBAC than Secrets, and show up unredacted in
// `kubectl describe`, GitOps diffs, and CI logs that dump manifests.
func CheckPlaintextSecretsInConfigMap(docs []ManifestDoc) []Finding {
	var findings []Finding
	for _, d := range docs {
		if d.Kind != "ConfigMap" {
			continue
		}
		for _, line := range strings.Split(d.Text, "\n") {
			m := suspiciousKeyRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			val := m[2]
			if strings.Contains(val, "${") || strings.Contains(val, "{{") {
				continue // templated / env-interpolated placeholder, not a literal secret
			}
			findings = append(findings, Finding{
				RuleID:   "CCD010",
				Severity: High,
				Title:    fmt.Sprintf("ConfigMap %s embeds what looks like a plaintext credential", d.Name),
				Path:     "ConfigMap/" + d.Name,
				Detail: fmt.Sprintf(
					"Line matches a credential-shaped key with a literal value: %q. ConfigMaps are "+
						"commonly readable by broader RBAC than Secrets, and show up unredacted in "+
						"kubectl describe, GitOps diffs, and CI logs that dump manifests.",
					strings.TrimSpace(line)),
				Remediation: "Move this value into a Secret (existingSecret, or a Secret-backed " +
					"inline-secret rendering) rather than a ConfigMap-mounted config file.",
			})
		}
	}
	return findings
}
