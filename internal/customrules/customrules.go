// Package customrules loads and evaluates operator-defined check rules from a
// declarative YAML format, so a new misconfiguration pattern specific to one team's
// environment can be caught without a code release. The built-in rules in
// internal/rules stay reserved for patterns this project has verified against the
// real chart itself — a rule loaded from an arbitrary file has not been, so its ID
// space is kept visibly separate rather than blended into the CCD0xx/CCD1xx catalog.
package customrules

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

// reservedPrefix is the built-in rule ID namespace. A custom rule reusing it would be
// indistinguishable in output from a rule this project has actually verified against
// the real chart, which a community/operator-authored rule has not been.
const reservedPrefix = "CCD"

// Condition is the comparison a Rule performs against the value found at Path.
type Condition string

const (
	Exists    Condition = "exists"
	Absent    Condition = "absent"
	Equals    Condition = "equals"
	NotEquals Condition = "notEquals"
	Matches   Condition = "matches"
)

// Rule is one operator-defined check, evaluated against the same effective values tree
// this tool's built-in rules see. Path uses the same dot-separated, "[N]"-array-index
// syntax as internal/values.GetPath.
//
// equals/notEquals compare the value at Path against Value via its string
// representation (fmt.Sprintf("%v", ...)) — sufficient for the common cases (a bool,
// a string, a number) without needing YAML-type-aware comparison logic in a simple
// declarative format. equals/notEquals/matches all require Path to actually resolve;
// "the path is absent" is its own condition (absent) rather than something notEquals
// also happens to satisfy, so combining "absent OR wrong value" is two explicit rules,
// not an implicit corner case of one.
type Rule struct {
	ID          string `yaml:"id"`
	Severity    string `yaml:"severity"`
	Title       string `yaml:"title"`
	Path        string `yaml:"path"`
	Condition   string `yaml:"condition"`
	Value       string `yaml:"value,omitempty"`
	Detail      string `yaml:"detail,omitempty"`
	Remediation string `yaml:"remediation,omitempty"`

	re *regexp.Regexp // compiled once at load time for condition: matches
}

// File is the top-level shape of a --rules-from document.
type File struct {
	Rules []Rule `yaml:"rules"`
}

// Parse parses and validates a rules file already in memory. source is used only to
// make error messages identify which file/URL was at fault.
func Parse(data []byte, source string) (File, error) {
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", source, err)
	}
	for i := range f.Rules {
		if err := f.Rules[i].validate(); err != nil {
			return File{}, fmt.Errorf("%s: rule %d: %w", source, i, err)
		}
	}
	return f, nil
}

func (r *Rule) validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("missing required field id")
	}
	if strings.HasPrefix(strings.ToUpper(r.ID), reservedPrefix) {
		return fmt.Errorf("id %q uses the reserved %q prefix (built-in rules only)", r.ID, reservedPrefix)
	}
	switch strings.ToLower(r.Severity) {
	case "critical", "high", "medium", "low":
	default:
		return fmt.Errorf("id %s: severity must be critical|high|medium|low, got %q", r.ID, r.Severity)
	}
	if strings.TrimSpace(r.Path) == "" {
		return fmt.Errorf("id %s: missing required field path", r.ID)
	}
	switch Condition(r.Condition) {
	case Exists, Absent:
	case Equals, NotEquals:
		if r.Value == "" {
			return fmt.Errorf("id %s: condition %s requires a non-empty value", r.ID, r.Condition)
		}
	case Matches:
		if r.Value == "" {
			return fmt.Errorf("id %s: condition matches requires a non-empty value (a regex)", r.ID)
		}
		re, err := regexp.Compile(r.Value)
		if err != nil {
			return fmt.Errorf("id %s: condition matches: invalid regex %q: %w", r.ID, r.Value, err)
		}
		r.re = re
	default:
		return fmt.Errorf("id %s: condition must be exists|absent|equals|notEquals|matches, got %q", r.ID, r.Condition)
	}
	return nil
}

func (r Rule) severity() rules.Severity {
	switch strings.ToLower(r.Severity) {
	case "critical":
		return rules.Critical
	case "high":
		return rules.High
	case "medium":
		return rules.Medium
	default:
		return rules.Low
	}
}

// Evaluate runs every rule in the file against v, returning one Finding per rule whose
// condition holds.
func (f File) Evaluate(v values.Values) []rules.Finding {
	var out []rules.Finding
	for _, r := range f.Rules {
		val, ok := values.GetPath(v, r.Path)
		fire := false
		switch Condition(r.Condition) {
		case Exists:
			fire = ok
		case Absent:
			fire = !ok
		case Equals:
			fire = ok && fmt.Sprintf("%v", val) == r.Value
		case NotEquals:
			fire = ok && fmt.Sprintf("%v", val) != r.Value
		case Matches:
			fire = ok && r.re != nil && r.re.MatchString(fmt.Sprintf("%v", val))
		}
		if !fire {
			continue
		}
		out = append(out, rules.Finding{
			RuleID:      r.ID,
			Severity:    r.severity(),
			Title:       r.Title,
			Path:        r.Path,
			Detail:      r.Detail,
			Remediation: r.Remediation,
		})
	}
	return out
}

// LoadFrom reads and validates a rules file from a local path or an http(s):// URL.
func LoadFrom(pathOrURL string) (File, error) {
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(pathOrURL) //nolint:gosec // operator-supplied URL is the whole point of --rules-from
		if err != nil {
			return File{}, fmt.Errorf("fetching %s: %w", pathOrURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return File{}, fmt.Errorf("fetching %s: unexpected status %s", pathOrURL, resp.Status)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return File{}, fmt.Errorf("reading %s: %w", pathOrURL, err)
		}
		return Parse(data, pathOrURL)
	}
	data, err := os.ReadFile(pathOrURL)
	if err != nil {
		return File{}, fmt.Errorf("reading %s: %w", pathOrURL, err)
	}
	return Parse(data, pathOrURL)
}
