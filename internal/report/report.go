// Package report formats findings for a terminal or for machine consumption (CI), and
// computes an exit code so a pipeline can gate on severity.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

// Sort orders findings most-severe first, stably.
func Sort(findings []rules.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		return rules.RankOf(findings[i].Severity) < rules.RankOf(findings[j].Severity)
	})
}

const (
	colReset  = "\x1b[0m"
	colBold   = "\x1b[1m"
	colRed    = "\x1b[31m"
	colOrange = "\x1b[33m"
	colGreen  = "\x1b[32m"
)

func colorFor(s rules.Severity) string {
	switch s {
	case rules.Critical:
		return colBold + colRed
	case rules.High:
		return colRed
	case rules.Medium:
		return colOrange
	default:
		return colGreen
	}
}

func writeFinding(w io.Writer, f rules.Finding, useColor bool) {
	c, reset := "", ""
	if useColor {
		c, reset = colorFor(f.Severity), colReset
	}
	fmt.Fprintf(w, "%s[%s]%s %s  (%s)\n", c, f.Severity, reset, f.Title, f.RuleID)
	if f.Path != "" {
		fmt.Fprintf(w, "  path: %s\n", f.Path)
	}
	if f.Detail != "" {
		fmt.Fprintf(w, "  %s\n", f.Detail)
	}
	if f.Remediation != "" {
		fmt.Fprintf(w, "  fix:  %s\n", f.Remediation)
	}
	fmt.Fprintln(w)
}

// WriteText renders findings as human-readable text. suppressed findings never
// silently vanish: their count is always in the summary line, and their detail is
// printed too when showSuppressed is true.
func WriteText(w io.Writer, findings, suppressed []rules.Finding, showSuppressed, useColor bool) {
	Sort(findings)
	Sort(suppressed)
	counts := map[rules.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	suffix := ""
	if len(suppressed) > 0 {
		suffix = fmt.Sprintf(", %d suppressed", len(suppressed))
		if !showSuppressed {
			suffix += " (see --show-suppressed)"
		}
	}
	if len(findings) == 0 {
		fmt.Fprintf(w, "No findings — every check passed%s.\n", suffix)
		fmt.Fprintln(w, "(Passing does not certify this deployment as production-ready — see README.)")
	} else {
		fmt.Fprintf(w, "%d finding(s): %d critical, %d high, %d medium, %d low%s\n\n",
			len(findings), counts[rules.Critical], counts[rules.High], counts[rules.Medium], counts[rules.Low], suffix)
		for _, f := range findings {
			writeFinding(w, f, useColor)
		}
	}
	if showSuppressed && len(suppressed) > 0 {
		fmt.Fprintln(w, "--- suppressed (still true, acknowledged via .chartdoctor-ignore.yaml) ---")
		for _, f := range suppressed {
			writeFinding(w, f, useColor)
		}
	}
}

// Result is the JSON/SARIF-adjacent machine-readable shape: kept findings plus a
// suppression count that is always present, so a suppression file can never make a
// problem invisible to a script parsing this output — only unenforced.
type Result struct {
	Findings        []rules.Finding `json:"findings"`
	SuppressedCount int             `json:"suppressedCount"`
	Suppressed      []rules.Finding `json:"suppressed,omitempty"`
}

// WriteJSON renders findings (and, if requested, suppressed findings) as JSON for
// CI/machine consumption.
func WriteJSON(w io.Writer, findings, suppressed []rules.Finding, showSuppressed bool) error {
	Sort(findings)
	Sort(suppressed)
	if findings == nil {
		findings = []rules.Finding{}
	}
	res := Result{Findings: findings, SuppressedCount: len(suppressed)}
	if showSuppressed {
		res.Suppressed = suppressed
		if res.Suppressed == nil {
			res.Suppressed = []rules.Finding{}
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// WriteJSONValue emits an arbitrary payload as indented JSON. The upgrade command
// carries more than a findings array (hops, runbook, migrated values), so it needs a
// writer that is not shaped around []rules.Finding.
func WriteJSONValue(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
