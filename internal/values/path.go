package values

import (
	"sort"
	"strings"
)

// SetPath writes value at a dotted path, creating intermediate maps as needed.
func SetPath(root Values, path string, value interface{}) {
	parts := strings.Split(path, ".")
	cur := map[string]interface{}(root)
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

// DeletePath removes the leaf at a dotted path and then prunes any parent map the
// removal left empty, so a rewritten values file doesn't accumulate hollow scaffolding
// like "global: {license: {}}" — which reads as a deliberate empty override to anyone
// reviewing the diff. Reports whether anything was removed.
func DeletePath(root Values, path string) bool {
	parts := strings.Split(path, ".")
	chain := []map[string]interface{}{map[string]interface{}(root)}
	cur := map[string]interface{}(root)
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			return false
		}
		chain = append(chain, next)
		cur = next
	}
	leaf := parts[len(parts)-1]
	if _, ok := cur[leaf]; !ok {
		return false
	}
	delete(cur, leaf)
	for i := len(chain) - 1; i > 0; i-- {
		if len(chain[i]) > 0 {
			break
		}
		delete(chain[i-1], parts[i-1])
	}
	return true
}

// DeepCopy returns a copy of v that shares no maps or slices with the original, so a
// caller can rewrite one copy while still reporting against the untouched input.
func DeepCopy(v Values) Values {
	out := make(Values, len(v))
	for k, val := range v {
		out[k] = deepCopyValue(val)
	}
	return out
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = deepCopyValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = deepCopyValue(val)
		}
		return out
	default:
		return v
	}
}

// MoveSubtree moves every leaf under srcPrefix to the same relative position under
// dstPrefix, then removes srcPrefix. It never overwrites a value that already exists at
// the destination; those paths are returned as conflicts so a human can decide, because
// silently preferring one side would change a deployment's behaviour without saying so.
//
// The Camunda chart expresses several renames as subtree moves ("camundaHub.webModeler.*"
// -> "camundaHub.*"), including cases where the destination is an ancestor of the source.
func MoveSubtree(root Values, srcPrefix, dstPrefix string) (moved []string, conflicts []string) {
	raw, ok := GetPath(root, srcPrefix)
	if !ok {
		return nil, nil
	}
	// Snapshot before mutating: the destination can be an ancestor of the source, so
	// writing while walking the live tree would read back half-moved state.
	leaves := map[string]interface{}{}
	collectLeaves(raw, nil, leaves)

	for rel, v := range leaves {
		dst := dstPrefix
		if rel != "" {
			dst = dstPrefix + "." + rel
		}
		if _, exists := GetPath(root, dst); exists {
			conflicts = append(conflicts, dst)
			continue
		}
		SetPath(root, dst, v)
		moved = append(moved, dst)
	}
	DeletePath(root, srcPrefix)
	sort.Strings(moved)
	sort.Strings(conflicts)
	return moved, conflicts
}

// collectLeaves flattens a value into dotted relative paths. A leaf is anything that is
// not a non-empty map, so an explicitly empty map is preserved as a value rather than
// vanishing during a move.
func collectLeaves(v interface{}, path []string, out map[string]interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		out[strings.Join(path, ".")] = v
		return
	}
	for k, child := range m {
		collectLeaves(child, append(append([]string{}, path...), k), out)
	}
}
