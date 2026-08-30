// Package instdiff computes a structural diff between two Helm values trees — added,
// removed, and changed keys with their old/new values — so two installs (e.g. staging
// vs prod) can be compared on what actually differs. Diffing the PARSED trees rather
// than the raw YAML text means key reordering between two `helm get values` dumps
// never shows up as noise the way a line-based text diff would report it.
package instdiff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Kind is what changed at a given path between the two trees.
type Kind string

const (
	Added   Kind = "added"   // present in B, not in A
	Removed Kind = "removed" // present in A, not in B
	Changed Kind = "changed" // present in both, with different values
)

// Change is one leaf-level difference. A and B hold the value on each side — only the
// side(s) relevant to Kind are populated (e.g. Added has only B).
type Change struct {
	Path string      `json:"path"`
	Kind Kind        `json:"kind"`
	A    interface{} `json:"a,omitempty"`
	B    interface{} `json:"b,omitempty"`
}

// Compare walks both trees and returns every difference, sorted by path. Arrays are
// compared as opaque values (reflect.DeepEqual), not element-by-element — this
// matches Helm's own array semantics (an overlay replaces an array wholesale, it
// never merges into one), so a diff at the array level is the meaningful unit, not a
// per-index one.
func Compare(a, b map[string]interface{}) []Change {
	var out []Change
	walk(a, b, nil, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walk(a, b map[string]interface{}, path []string, out *[]Change) {
	keys := make(map[string]bool, len(a)+len(b))
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, k := range sorted {
		av, aok := a[k]
		bv, bok := b[k]
		p := append(append([]string{}, path...), k)
		full := strings.Join(p, ".")

		switch {
		case aok && !bok:
			*out = append(*out, Change{Path: full, Kind: Removed, A: av})
		case !aok && bok:
			*out = append(*out, Change{Path: full, Kind: Added, B: bv})
		default:
			am, aIsMap := av.(map[string]interface{})
			bm, bIsMap := bv.(map[string]interface{})
			if aIsMap && bIsMap {
				walk(am, bm, p, out)
				continue
			}
			if !reflect.DeepEqual(av, bv) {
				*out = append(*out, Change{Path: full, Kind: Changed, A: av, B: bv})
			}
		}
	}
}

// WriteText renders changes as a human-readable diff, labelled with the two release
// names being compared.
func WriteText(changes []Change, nameA, nameB string) string {
	var b strings.Builder
	if len(changes) == 0 {
		fmt.Fprintf(&b, "No differences between %s and %s.\n", nameA, nameB)
		return b.String()
	}
	fmt.Fprintf(&b, "%d difference(s) between %s and %s:\n\n", len(changes), nameA, nameB)
	for _, c := range changes {
		switch c.Kind {
		case Added:
			fmt.Fprintf(&b, "+ %s: %v\n", c.Path, c.B)
		case Removed:
			fmt.Fprintf(&b, "- %s: %v\n", c.Path, c.A)
		case Changed:
			fmt.Fprintf(&b, "~ %s: %v -> %v\n", c.Path, c.A, c.B)
		}
	}
	return b.String()
}
