// Package bundle collects everything a support engineer would otherwise ask for
// across several ticket replies — redacted values, describe/events/logs, versions —
// into one reviewable archive a customer can attach when opening a ticket.
package bundle

import "regexp"

// sensitiveKeyRe matches a map key that looks credential-shaped: "password",
// "secret", "token", or "credential" ANYWHERE in the key, or "key" specifically at
// the END of the key (so e.g. "existingSecretKey" is caught by the trailing "key",
// while a key that merely contains "key" as a substring in the middle is not
// blanket-redacted just for that). Case-insensitive because this chart's schema
// mixes camelCase and lower-case conventions.
var sensitiveKeyRe = regexp.MustCompile(`(?i)(password|secret|token|credential|key$)`)

// RedactValues returns a deep copy of v with every value reachable under a
// credential-shaped key replaced by "<redacted>". It never redacts key NAMES, only
// values, and never collapses a matched key's substructure into a single string —
// if a "secret:" block is itself a map (existingSecret/existingSecretKey/
// inlineSecret siblings), every scalar leaf within it is individually redacted so
// the surrounding YAML stays structurally valid. This intentionally also redacts
// non-sensitive sibling fields like existingSecret's target name once the parent
// key matches (see package doc) — a deliberate over-redaction, not an oversight:
// this package's one job is "never let a real secret value leak", and getting that
// wrong is worse than being slightly less informative.
func RedactValues(v map[string]interface{}) map[string]interface{} {
	out, _ := redactValue(v, false).(map[string]interface{})
	return out
}

// redactValue recurses through the tree. underSensitiveKey is true once any
// ancestor key matched sensitiveKeyRe — once true, every scalar leaf below it is
// replaced with "<redacted>", regardless of that leaf's own key name.
func redactValue(v interface{}, underSensitiveKey bool) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = redactValue(val, underSensitiveKey || sensitiveKeyRe.MatchString(k))
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = redactValue(val, underSensitiveKey)
		}
		return out
	default:
		if underSensitiveKey {
			return "<redacted>"
		}
		return t
	}
}
