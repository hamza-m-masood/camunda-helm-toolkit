// Package chartschema resolves a curated set of values.yaml field paths against a
// real chart's own `## @param`/`## @extra` annotations, so `init` can confirm the exact
// paths it writes into still exist and still mean what it thinks they mean on whatever
// chart version it is pointed at — the same "cannot disagree with the chart" discipline
// internal/upgrade uses for constraints.tpl, applied to values.yaml's own doc comments.
//
// This is deliberately not a general-purpose schema-for-every-field extractor: `init`
// asks a fixed, curated list of questions, not a dynamically generated form over
// hundreds of fields, so all it needs is a validated lookup for the paths it actually
// writes to, not a complete UI-driving schema.
//
// Known, accepted gap: a handful of `## @param` comments in the real chart document a
// Spring Boot logging-level map whose YAML key is itself a literal string containing
// dots (e.g. "logging.level.io.camunda.identity" documents the key "io.camunda.identity"
// under logging.level, not three nested levels — see identity.logging.level.io.camunda.identity
// and its siblings in the real 8.9/8.10 values.yaml). Plain dot-notation cannot
// distinguish that from real nesting without either trying every split or hardcoding a
// list of known literal-dotted-key roots, and `init`'s curated question list never
// touches any logging.level.* field, so this package does not attempt to resolve them —
// Extract still returns them (Resolved will be false), callers just should not expect
// GetPath-style resolution to succeed for that one documented shape.
package chartschema

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

// Tag is which chart doc-comment annotated a field.
type Tag string

const (
	TagParam Tag = "param" // a leaf value a customer is expected to configure
	TagExtra Tag = "extra" // a container node, or a param the docs generator groups separately
	TagSkip  Tag = "skip"  // deliberately excluded from the chart's own generated docs/README
)

// Kind is a coarse type inferred from the field's default value in values.yaml.
type Kind string

const (
	KindBool   Kind = "bool"
	KindString Kind = "string"
	KindNumber Kind = "number"
	KindList   Kind = "list"
	KindMap    Kind = "map"
	KindNull   Kind = "null" // value is nil/empty in values.yaml (e.g. "" or {})
)

// Field is one `## @param`/`## @extra`/`## @skip` annotated line paired with the actual
// default value found at that path in the same values.yaml.
type Field struct {
	Path        string
	Tag         Tag
	Description string
	Default     interface{}
	Kind        Kind
	// Resolved is false only when the comment's own path does not resolve to
	// anything in the document at all (values.GetPath found nothing) — distinct
	// from Kind == KindNull, which just means the resolved value is empty
	// ("" or {}), a normal and common intentional default.
	Resolved bool
}

// annotationRe matches a chart doc-comment line: "## @param <path> <description...>",
// "## @extra <path> <description...>", or "## @skip <path>" with no description.
// Verified against the real chart (charts/camunda-platform-8.9/values.yaml,
// charts/camunda-platform-8.10/values.yaml): the annotated key's own YAML line always
// follows immediately below the comment, at any indentation depth — the path in the
// comment is the full dot-path from root, not just the local key name, so no separate
// indentation tracking is needed to reconstruct it.
var annotationRe = regexp.MustCompile(`^\s*##\s*@(param|extra|skip)\s+(\S+)(?:\s+(.*))?\s*$`)

// Extract parses raw values.yaml content and returns every annotated field, keyed by
// dot-path, with its default value resolved from the same document. A field whose
// comment path does not resolve to any actual value in the document is included with
// Default == nil and Kind == KindNull rather than dropped — see Field.Kind and the
// Unresolved helper, which callers can use to detect a chart having renamed or removed
// a field this tool still expects.
func Extract(valuesYAML []byte) (map[string]Field, error) {
	v, err := values.ParseYAML(valuesYAML)
	if err != nil {
		return nil, fmt.Errorf("parsing values.yaml: %w", err)
	}

	fields := make(map[string]Field)
	lines := strings.Split(string(valuesYAML), "\n")
	for _, line := range lines {
		m := annotationRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tag := Tag(m[1])
		path := m[2]
		desc := strings.TrimSpace(m[3])

		raw, ok := values.GetPath(v, path)
		kind := KindNull
		if ok && raw != nil {
			kind = kindOf(raw)
		}

		// A path can legitimately be annotated more than once across a large values.yaml
		// only in the sense of duplicate @extra section headers; last one wins, which
		// matches how the chart's own README generator resolves the same file.
		fields[path] = Field{
			Path:        path,
			Tag:         tag,
			Description: desc,
			Default:     raw,
			Kind:        kind,
			Resolved:    ok,
		}
	}
	return fields, nil
}

func kindOf(v interface{}) Kind {
	switch t := v.(type) {
	case bool:
		return KindBool
	case string:
		if t == "" {
			return KindNull
		}
		return KindString
	case int, int64, float64:
		return KindNumber
	case []interface{}:
		return KindList
	case map[string]interface{}:
		if len(t) == 0 {
			return KindNull
		}
		return KindMap
	default:
		return KindNull
	}
}

// Lookup resolves a single dot-path against an already-extracted field set.
func Lookup(fields map[string]Field, path string) (Field, bool) {
	f, ok := fields[path]
	return f, ok
}

// Unresolved returns every path in want that either has no @param/@extra annotation in
// fields at all, or whose comment path did not resolve to anything in the document
// (Field.Resolved == false) — the drift signal `init` uses to refuse to run against a
// chart whose values.yaml no longer has a field it depends on, rather than silently
// writing to a path the chart will ignore. A resolved field whose value happens to be
// empty (Kind == KindNull, e.g. existingSecret: "") is not drift and is not included.
func Unresolved(fields map[string]Field, want []string) []string {
	var missing []string
	for _, path := range want {
		f, ok := fields[path]
		if !ok || !f.Resolved {
			missing = append(missing, path)
		}
	}
	return missing
}
