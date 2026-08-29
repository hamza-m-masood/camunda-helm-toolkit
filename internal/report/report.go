// Package report formats findings for a terminal or for machine consumption (CI), and
// computes an exit code so a pipeline can gate on severity.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
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

// WriteText renders findings as human-readable text.
func WriteText(w io.Writer, findings []rules.Finding, useColor bool) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "No findings — every check passed.")
		fmt.Fprintln(w, "(Passing does not certify this deployment as production-ready — see README.)")
		return
	}
	Sort(findings)
	counts := map[rules.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	fmt.Fprintf(w, "%d finding(s): %d critical, %d high, %d medium, %d low\n\n",
		len(findings), counts[rules.Critical], counts[rules.High], counts[rules.Medium], counts[rules.Low])
	for _, f := range findings {
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
}

// WriteJSON renders findings as a JSON array for CI/machine consumption.
func WriteJSON(w io.Writer, findings []rules.Finding) error {
	Sort(findings)
	if findings == nil {
		findings = []rules.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}
