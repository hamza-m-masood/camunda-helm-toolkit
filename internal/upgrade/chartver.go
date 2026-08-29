// Package upgrade plans a Camunda 8 Self-Managed chart upgrade: which values keys the
// target line removed, renamed or deprecated, and which imperative steps no values file
// can express.
//
// The key data is generated from the chart's own deprecation helpers rather than
// maintained by hand — see extract.go — so it tracks the chart instead of drifting from
// it. The imperative steps are transcribed from the chart repo's own CI upgrade hooks,
// which means every step in the runbook is one the chart's test matrix actually runs.
package upgrade

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Line is a Camunda minor version line, e.g. 8.10. This is the form the chart uses for
// its directory names (charts/camunda-platform-8.10) and the form users recognise.
type Line struct {
	Major int
	Minor int
}

var lineRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// ParseLine accepts "8.10", "8.10.2", or "camunda-platform-8.10".
func ParseLine(s string) (Line, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "camunda-platform-")
	m := lineRe.FindStringSubmatch(s)
	if m == nil {
		return Line{}, fmt.Errorf("not a Camunda version line: %q (expected e.g. 8.10)", s)
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return Line{Major: maj, Minor: min}, nil
}

func (l Line) String() string { return fmt.Sprintf("%d.%d", l.Major, l.Minor) }

func (l Line) Compare(o Line) int {
	if l.Major != o.Major {
		return l.Major - o.Major
	}
	return l.Minor - o.Minor
}

func (l Line) Less(o Line) bool { return l.Compare(o) < 0 }

// Hops enumerates the minor versions to pass through, excluding from and including to.
// Camunda does not support skipping minors, so 8.7 -> 8.10 is three separate upgrades
// and the caller needs to know that before planning one.
func Hops(from, to Line) ([]Line, error) {
	switch {
	case from.Compare(to) == 0:
		return nil, nil
	case to.Less(from):
		return nil, fmt.Errorf("target %s is older than current %s; this tool does not plan downgrades", to, from)
	case from.Major != to.Major:
		return nil, fmt.Errorf("cross-major upgrade %s -> %s is out of scope for this tool", from, to)
	}
	var out []Line
	for m := from.Minor + 1; m <= to.Minor; m++ {
		out = append(out, Line{Major: from.Major, Minor: m})
	}
	return out, nil
}

// chartMajorToLine maps a Helm chart major version to the Camunda line it ships. The
// offset is a historical fact per line rather than arithmetic, so it stays a table;
// verified against every charts/camunda-platform-*/Chart.yaml.
var chartMajorToLine = map[int]Line{
	8:  {8, 3},
	9:  {8, 4},
	10: {8, 5},
	11: {8, 6},
	12: {8, 7},
	13: {8, 8},
	14: {8, 9},
	15: {8, 10},
}

// LineFromChartVersion maps a chart semver ("14.8.5") to its Camunda line (8.9).
func LineFromChartVersion(chartVersion string) (Line, bool) {
	m := regexp.MustCompile(`^v?(\d+)\.`).FindStringSubmatch(strings.TrimSpace(chartVersion))
	if m == nil {
		return Line{}, false
	}
	maj, _ := strconv.Atoi(m[1])
	l, ok := chartMajorToLine[maj]
	return l, ok
}

// ChartMajorForLine is the inverse of LineFromChartVersion.
func ChartMajorForLine(l Line) (int, bool) {
	for maj, line := range chartMajorToLine {
		if line.Compare(l) == 0 {
			return maj, true
		}
	}
	return 0, false
}
