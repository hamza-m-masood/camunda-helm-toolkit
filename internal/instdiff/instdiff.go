// Package instdiff computes a structural diff between two Helm values trees — added,
// removed, and changed keys with their old/new values — so two installs (e.g. staging
// vs prod) can be compared on what actually differs. Diffing the PARSED trees rather
// than the raw YAML text means key reordering between two `helm get values` dumps
// never shows up as noise the way a line-based text diff would report it.
package instdiff

import (
	"encoding/json"
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

// Compare walks both trees and returns every difference, sorted by path. Arrays get
// their own comparison, not a blanket reflect.DeepEqual: Helm replaces an array
// wholesale rather than merging it, but the values being compared here are two
// releases' EFFECTIVE state (helm get values -a), not raw overlay files an operator
// would rewrite by hand — for that, "which element differs" is the useful unit, not
// "the array differs somewhere." Two lists with the exact same elements in a
// different order are reported as no difference at all (order is not semantically
// meaningful for a values.yaml list); same-length lists are compared position by
// position, so the reported path pinpoints the actual differing leaf (including any
// nested "[N]" index) rather than dumping both full lists; only a genuine length
// mismatch, where no positional alignment is meaningful without a real list-matching
// algorithm this project doesn't have a use case to justify, falls back to reporting
// the whole array as changed.
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
			compareValues(av, bv, p, out)
		}
	}
}

// compareValues dispatches on the shape of av/bv (map, list, or scalar) — the single
// place both walk's per-key comparison and diffList's per-index comparison go through,
// so a list nested inside a list, or a map nested inside a list, recurses the same way
// a map nested inside a map already does.
func compareValues(av, bv interface{}, p []string, out *[]Change) {
	am, aIsMap := av.(map[string]interface{})
	bm, bIsMap := bv.(map[string]interface{})
	if aIsMap && bIsMap {
		walk(am, bm, p, out)
		return
	}
	al, aIsList := av.([]interface{})
	bl, bIsList := bv.([]interface{})
	if aIsList && bIsList {
		diffList(al, bl, p, out)
		return
	}
	if !reflect.DeepEqual(av, bv) {
		*out = append(*out, Change{Path: strings.Join(p, "."), Kind: Changed, A: av, B: bv})
	}
}

// diffList compares two lists found at the same path. See Compare's doc comment for
// the reasoning behind order-insensitivity and the length-mismatch fallback.
func diffList(a, b []interface{}, path []string, out *[]Change) {
	if reflect.DeepEqual(a, b) {
		return
	}
	if sameElementsIgnoringOrder(a, b) {
		return
	}
	if len(a) != len(b) {
		*out = append(*out, Change{Path: strings.Join(path, "."), Kind: Changed, A: a, B: b})
		return
	}
	for i := range a {
		compareValues(a[i], b[i], append(append([]string{}, path...), fmt.Sprintf("[%d]", i)), out)
	}
}

// sameElementsIgnoringOrder reports whether a and b hold the same elements regardless
// of position, by comparing each element's canonical JSON encoding — encoding/json
// sorts map keys alphabetically, which is exactly the canonical form a values.yaml map
// needs for this comparison to be order-independent at every nesting level, not just
// the top one.
func sameElementsIgnoringOrder(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	as := canonicalStrings(a)
	bs := canonicalStrings(b)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func canonicalStrings(list []interface{}) []string {
	out := make([]string, len(list))
	for i, v := range list {
		b, err := json.Marshal(v)
		if err != nil {
			// Unmarshaled YAML scalars/maps/lists are always JSON-marshalable — this
			// is unreachable in practice, but never let a marshal error silently make
			// two different elements compare as equal strings ("").
			out[i] = fmt.Sprintf("<unmarshalable:%v>", v)
			continue
		}
		out[i] = string(b)
	}
	return out
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
