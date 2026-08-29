// Package values loads and merges Helm values.yaml documents using Helm's own merge
// semantics (maps merge recursively, everything else — including arrays — is replaced),
// and provides a schema-agnostic walker used by rules to find configuration patterns
// (e.g. every "resources" block, every "existingSecret" reference) without hardcoding
// the full Camunda values.yaml shape.
package values

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Values is a parsed, merged Helm values document.
type Values map[string]interface{}

// ParseYAML parses raw YAML bytes into a Values map.
func ParseYAML(data []byte) (Values, error) {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if raw == nil {
		return Values{}, nil
	}
	m, ok := normalize(raw).(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("root of values document is not a mapping")
	}
	return Values(m), nil
}

// normalize recursively converts map[interface{}]interface{} (which some YAML decoders
// produce) into map[string]interface{}, so downstream code only ever deals with one shape.
func normalize(in interface{}) interface{} {
	switch v := in.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			out[k] = normalize(val)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			out[fmt.Sprintf("%v", k)] = normalize(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, val := range v {
			out[i] = normalize(val)
		}
		return out
	default:
		return v
	}
}

// DeepMerge merges src into dst per Helm's own semantics: maps merge key-by-key and
// recurse; anything else (scalars, arrays) in src replaces the value in dst wholesale.
// dst is not mutated; a new Values is returned.
func DeepMerge(dst, src Values) Values {
	out := make(Values, len(dst)+len(src))
	for k, v := range dst {
		out[k] = v
	}
	for k, sv := range src {
		if dv, ok := out[k]; ok {
			dm, dIsMap := dv.(map[string]interface{})
			sm, sIsMap := sv.(map[string]interface{})
			if dIsMap && sIsMap {
				out[k] = map[string]interface{}(DeepMerge(Values(dm), Values(sm)))
				continue
			}
		}
		out[k] = sv
	}
	return out
}

// Block is one map node found while walking a Values tree, along with its dotted path
// from the root (array indices rendered as "[i]").
type Block struct {
	Path string
	Node map[string]interface{}
}

// FindBlocks walks the entire values tree and returns every map node that contains ALL
// of requiredKeys (regardless of their values). This is how rules stay schema-agnostic:
// e.g. FindBlocks(v, "requests", "limits") finds every resources block in the chart,
// under any component, at any nesting depth, without the rule needing to know the
// component list in advance.
func FindBlocks(root Values, requiredKeys ...string) []Block {
	var out []Block
	var walk func(node interface{}, path []string)
	walk = func(node interface{}, path []string) {
		switch m := node.(type) {
		case map[string]interface{}:
			has := true
			for _, k := range requiredKeys {
				if _, ok := m[k]; !ok {
					has = false
					break
				}
			}
			if has {
				p := make([]string, len(path))
				copy(p, path)
				out = append(out, Block{Path: strings.Join(p, "."), Node: m})
			}
			for k, v := range m {
				walk(v, append(append([]string{}, path...), k))
			}
		case []interface{}:
			for i, v := range m {
				walk(v, append(append([]string{}, path...), fmt.Sprintf("[%d]", i)))
			}
		}
	}
	walk(map[string]interface{}(root), nil)
	return out
}

// GetPath resolves a dotted path (e.g. "orchestration.podDisruptionBudget.enabled")
// against the values tree. The second return value is false if any segment is missing.
func GetPath(root Values, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var cur interface{} = map[string]interface{}(root)
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// ToInt coerces a YAML-decoded scalar (int, float64, or a numeric string — the Camunda
// chart quotes several ints like replicationFactor/clusterSize) into an int.
func ToInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	default:
		return 0, fmt.Errorf("value %#v is not an integer", v)
	}
}
