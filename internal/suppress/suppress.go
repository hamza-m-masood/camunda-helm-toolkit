// Package suppress lets an operator formally accept a specific finding — a real,
// deliberate tradeoff (e.g. intentional Burstable QoS on one component) — without
// losing visibility that a suppression exists. A suppressed finding is still counted
// and remains listable; it never just silently disappears.
package suppress

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

// Entry is one suppression rule: match on RuleID (required) and, optionally, an
// exact-or-dot-prefix match on the finding's Path. Reason is required so the file
// stays self-documenting and auditable — a suppression with no stated reason is
// rejected at load time rather than silently accepted.
type Entry struct {
	RuleID string `yaml:"ruleId"`
	Path   string `yaml:"path,omitempty"`
	Reason string `yaml:"reason"`
}

// File is the top-level .chartdoctor-ignore.yaml shape.
type File struct {
	Suppress []Entry `yaml:"suppress"`
}

// Load reads and validates a suppression file. A missing file is a plain os error —
// callers decide whether that's fatal or means "no suppressions configured".
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, e := range f.Suppress {
		if strings.TrimSpace(e.RuleID) == "" {
			return File{}, fmt.Errorf("%s: entry %d is missing required field ruleId", path, i)
		}
		if strings.TrimSpace(e.Reason) == "" {
			return File{}, fmt.Errorf("%s: entry %d (rule %s) is missing a required reason", path, i, e.RuleID)
		}
	}
	return f, nil
}

func (e Entry) matches(f rules.Finding) bool {
	if e.RuleID != f.RuleID {
		return false
	}
	if e.Path == "" {
		return true
	}
	return f.Path == e.Path || strings.HasPrefix(f.Path, e.Path+".")
}

// Apply splits findings into (kept, suppressed) using the file's entries. kept
// preserves the original order; suppressed does too.
func (f File) Apply(findings []rules.Finding) (kept, suppressed []rules.Finding) {
	for _, finding := range findings {
		matched := false
		for _, e := range f.Suppress {
			if e.matches(finding) {
				matched = true
				break
			}
		}
		if matched {
			suppressed = append(suppressed, finding)
		} else {
			kept = append(kept, finding)
		}
	}
	return kept, suppressed
}
