package upgrade

import (
	"embed"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed runbookdata/*.yaml
var runbookData embed.FS

// Steps are only added for behaviour verified in the chart repo, and each carries the
// file it was transcribed from. A hop with no verified imperative work reports exactly
// that; inventing plausible-looking steps would be worse than saying nothing, because
// an operator cannot tell the difference.

// StepKind groups a step by what the operator is being asked to do.
type StepKind string

const (
	StepPrerequisite StepKind = "prerequisite"
	StepManual       StepKind = "manual"
	StepVerify       StepKind = "verify"
	// StepContingency is not part of the happy path. It is printed as "if X happens,
	// do Y" so nobody runs a destructive remedy pre-emptively.
	StepContingency StepKind = "contingency"
)

// StepDanger tells the operator what a step risks before they run it.
type StepDanger string

const (
	DangerSafe        StepDanger = "safe"
	DangerDestructive StepDanger = "destructive"
	DangerDowntime    StepDanger = "downtime"
)

// Step is one runbook instruction.
type Step struct {
	ID       string     `yaml:"id" json:"id"`
	Title    string     `yaml:"title" json:"title"`
	Kind     StepKind   `yaml:"kind" json:"kind"`
	Danger   StepDanger `yaml:"danger" json:"danger"`
	When     string     `yaml:"when" json:"when,omitempty"`
	Why      string     `yaml:"why" json:"why,omitempty"`
	Commands []string   `yaml:"commands" json:"commands,omitempty"`
	Docs     string     `yaml:"docs,omitempty" json:"docs,omitempty"`
	Source   string     `yaml:"source,omitempty" json:"source,omitempty"`
	// Hop is the target line the step belongs to, or "common".
	Hop string `yaml:"-" json:"hop"`
}

type runbookFile struct {
	Hop   string `yaml:"hop"`
	Steps []Step `yaml:"steps"`
}

// Facts are the cluster observations that decide which conditional steps apply.
type Facts struct {
	BundledKeycloak      bool `json:"bundledKeycloak"`
	BundledPostgres      bool `json:"bundledPostgres"`
	BundledElasticsearch bool `json:"bundledElasticsearch"`
}

func (f Facts) bundledBitnami() bool {
	return f.BundledKeycloak || f.BundledPostgres || f.BundledElasticsearch
}

func (f Facts) matches(when string) bool {
	switch when {
	case "", "always":
		return true
	case "bundled-bitnami":
		return f.bundledBitnami()
	case "bundled-keycloak":
		return f.BundledKeycloak
	case "bundled-postgres":
		return f.BundledPostgres
	case "bundled-elasticsearch":
		return f.BundledElasticsearch
	default:
		// An unrecognised condition shows the step rather than hiding it: a step the
		// operator does not need is a smaller failure than one they never see.
		return true
	}
}

// Substitutions fill the placeholders in step commands.
type Substitutions struct {
	Release            string
	Namespace          string
	TargetChartVersion string
	MigratedValues     string
}

// RunbookFor returns the applicable steps for a set of hops, common steps first and
// contingencies last.
func RunbookFor(hops []string, facts Facts, subs Substitutions) ([]Step, error) {
	byHop, err := loadRunbook()
	if err != nil {
		return nil, err
	}
	var main, contingencies []Step
	for _, h := range append([]string{"common"}, hops...) {
		for _, s := range byHop[h] {
			if !facts.matches(s.When) {
				continue
			}
			s.Hop = h
			s.Commands = substitute(s.Commands, subs)
			s.Why = strings.TrimRight(s.Why, "\n")
			if s.Kind == StepContingency {
				contingencies = append(contingencies, s)
				continue
			}
			main = append(main, s)
		}
	}
	return append(main, contingencies...), nil
}

// HopsWithSteps reports which hops have runbook data, so the caller can state plainly
// that a hop contributes no imperative work rather than leaving the operator to guess.
func HopsWithSteps(hops []string) (map[string]bool, error) {
	byHop, err := loadRunbook()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(hops))
	for _, h := range hops {
		out[h] = len(byHop[h]) > 0
	}
	return out, nil
}

func loadRunbook() (map[string][]Step, error) {
	entries, err := runbookData.ReadDir("runbookdata")
	if err != nil {
		return nil, err
	}
	out := map[string][]Step{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := runbookData.ReadFile(path.Join("runbookdata", e.Name()))
		if err != nil {
			return nil, err
		}
		var f runbookFile
		if err := yaml.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("parsing runbook %s: %w", e.Name(), err)
		}
		if f.Hop == "" {
			return nil, fmt.Errorf("runbook %s has no hop field", e.Name())
		}
		out[f.Hop] = append(out[f.Hop], f.Steps...)
	}
	return out, nil
}

func substitute(cmds []string, s Substitutions) []string {
	repl := [][2]string{
		{"{{RELEASE}}", orDefault(s.Release, "<release>")},
		{"{{NAMESPACE}}", orDefault(s.Namespace, "<namespace>")},
		{"{{TARGET_CHART_VERSION}}", orDefault(s.TargetChartVersion, "<chart-version>")},
		{"{{MIGRATED_VALUES}}", orDefault(s.MigratedValues, "migrated-values.yaml")},
	}
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		for _, r := range repl {
			c = strings.ReplaceAll(c, r[0], r[1])
		}
		out = append(out, c)
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
