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
		// The {name: "X", value: "Y"} idiom (the standard Kubernetes/Helm env-var
		// shape — orchestration.env, extraEnvVars, etc.) hides the sensitive signal in
		// name's VALUE, not in any map key: "name" and "value" are themselves
		// innocuous key names, so the walk below would otherwise never notice. If
		// name's value looks credential-shaped, redact the sibling "value" field
		// directly, independent of whatever ancestor key this map sits under.
		envValueRedact := false
		if name, ok := t["name"].(string); ok {
			if _, hasValue := t["value"]; hasValue {
				envValueRedact = sensitiveKeyRe.MatchString(name)
			}
		}

		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if envValueRedact && k == "value" {
				out[k] = "<redacted>"
				continue
			}
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

// describeLineRe matches one "kubectl describe"-style "Key:  value" line: a single
// whitespace-free key token immediately followed by a colon, then the value after
// however much column-alignment padding kubectl used (its width varies with the
// longest key in that block, so this can't assume a fixed number of spaces — but that
// padding is horizontal only: it must not include \n, or the pattern bridges across an
// unrelated heading line straight into the next line's content and misattributes it).
// A "Some Words:" heading (a space inside the key, as kubectl uses for section titles
// like "Restart Count:") never matches — [^\s:]+ can't cross the embedded space — so
// this only ever fires on single-token keys, exactly the shape a real env var name has.
var describeLineRe = regexp.MustCompile(`(?m)^([ \t]*)([^\s:]+):([ \t]+)(.*)$`)

// RedactDescribeText applies the same credential-shaped-key policy RedactValues uses
// on structured YAML to free-form "kubectl describe" text, which has no structure to
// walk. This exists specifically because `kubectl describe pod` prints every
// container's env vars verbatim under "Environment:" — a literal (non-secretKeyRef)
// value there is not otherwise redacted anywhere else in this package.
func RedactDescribeText(s string) string {
	return describeLineRe.ReplaceAllStringFunc(s, func(line string) string {
		m := describeLineRe.FindStringSubmatch(line)
		if m == nil || !sensitiveKeyRe.MatchString(m[2]) {
			return line
		}
		indent, key, sep := m[1], m[2], m[3]
		return indent + key + ":" + sep + "<redacted>"
	})
}
