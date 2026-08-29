package values

import (
	"strconv"
	"strings"
)

// suffix multipliers, longest suffix first so "Ki" is tried before a bare "K" would
// wrongly consume it.
var quantitySuffixes = []struct {
	suf string
	mul float64
}{
	{"Ei", 1 << 60}, {"Pi", 1 << 50}, {"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10},
	{"E", 1e18}, {"P", 1e15}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"k", 1e3}, {"K", 1e3},
	{"m", 1e-3},
}

// ParseQuantity parses a Kubernetes-style resource quantity string (e.g. "1500Mi",
// "2Gi", "500m", "2") into a float64 in base units (bytes for memory, whole cores for
// CPU). It is intentionally a minimal parser — enough for ratio comparisons between two
// quantities of the same kind, not a full apimachinery-compatible implementation.
func ParseQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	for _, suf := range quantitySuffixes {
		if strings.HasSuffix(s, suf.suf) {
			numPart := strings.TrimSuffix(s, suf.suf)
			n, err := strconv.ParseFloat(numPart, 64)
			if err != nil {
				return 0, false
			}
			return n * suf.mul, true
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
