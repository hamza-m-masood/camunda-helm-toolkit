package upgrade

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/*.yaml
var manifestData embed.FS

// Action is what the chart does when it finds the key in a user's values.
type Action string

const (
	// ActionRemoved: the chart calls fail() and renders nothing. Blocks the upgrade.
	ActionRemoved Action = "removed"
	// ActionRenamed: also a fail(), but the chart names the replacement key, so the
	// fix is mechanical.
	ActionRenamed Action = "renamed"
	// ActionDeprecated: the chart warns and keeps working.
	ActionDeprecated Action = "deprecated"
)

// Trigger records what makes the chart notice a key, so the planner can reproduce the
// chart's own condition instead of flagging every key that merely appears in a values
// file. Getting this wrong in the lenient direction produces false alarms on defaults;
// getting it wrong in the strict direction hides real blockers.
type Trigger string

const (
	// TriggerPresence: the chart checks hasKey, so presence alone is enough.
	TriggerPresence Trigger = "presence"
	// TriggerNotDefault: the chart compares against a default; a user who set the key
	// to that same default is unaffected.
	TriggerNotDefault Trigger = "not-default"
	// TriggerNonEmpty: fires on a non-empty string, list or map.
	TriggerNonEmpty Trigger = "non-empty"
	// TriggerTruthy: fires when the value is truthy.
	TriggerTruthy Trigger = "truthy"
	// TriggerFalsy: fires when the value is explicitly false, used where the chart
	// deprecates opting out of a default-on feature.
	TriggerFalsy Trigger = "falsy"
	// TriggerUnknown: the condition was too complex to classify. The planner falls
	// back to presence and marks the finding approximate rather than guessing silently.
	TriggerUnknown Trigger = "unknown"
)

// Key is one values.yaml key change in one chart line.
type Key struct {
	Old string `yaml:"old"`
	New string `yaml:"new,omitempty"`
	// Migration names where the setting now belongs when there is no one-to-one
	// replacement key — usually a component's extraConfiguration.
	Migration string  `yaml:"migration,omitempty"`
	Action    Action  `yaml:"action"`
	Trigger   Trigger `yaml:"trigger"`
	Default   string  `yaml:"default,omitempty"`
	// Condition is the raw Helm expression the entry came from, kept so a human can
	// audit why a key was flagged and so unclassified triggers stay reviewable.
	Condition string `yaml:"condition,omitempty"`
	RemovedIn string `yaml:"removedIn,omitempty"`
	URL       string `yaml:"url,omitempty"`
	// Source is the chart template and line this was generated from.
	Source string `yaml:"source,omitempty"`
}

// Blocking reports whether leaving this key in place fails `helm upgrade`.
func (k Key) Blocking() bool { return k.Action == ActionRemoved || k.Action == ActionRenamed }

// wildcardSuffix marks a key that names a whole subtree rather than one leaf. The chart
// writes these as "camundaHub.webModeler.*".
const wildcardSuffix = ".*"

// IsSubtree reports whether this key names a subtree rather than a single value.
func (k Key) IsSubtree() bool { return strings.HasSuffix(k.Old, wildcardSuffix) }

// OldPrefix is the values path the key refers to, with any wildcard suffix removed.
func (k Key) OldPrefix() string { return strings.TrimSuffix(k.Old, wildcardSuffix) }

// NewPrefix is the replacement path with any wildcard suffix removed.
func (k Key) NewPrefix() string { return strings.TrimSuffix(k.New, wildcardSuffix) }

// Manifest is every key change for one Camunda line.
type Manifest struct {
	Line       string `yaml:"line"`
	ChartMajor int    `yaml:"chartMajor"`
	// RequiresHelmMajor is the minimum Helm CLI major the chart hard-fails without,
	// or 0 when the chart imposes none.
	RequiresHelmMajor int `yaml:"requiresHelmMajor,omitempty"`
	// Support is the lifecycle bucket from the chart repo's charts/chart-versions.yaml.
	Support string `yaml:"support,omitempty"`
	// GeneratedFrom is the chart-repo commit the data was extracted at.
	GeneratedFrom string `yaml:"generatedFrom,omitempty"`
	Keys          []Key  `yaml:"keys"`
}

// Counts summarises a manifest for display.
func (m Manifest) Counts() (removed, renamed, deprecated int) {
	for _, k := range m.Keys {
		switch k.Action {
		case ActionRemoved:
			removed++
		case ActionRenamed:
			renamed++
		case ActionDeprecated:
			deprecated++
		}
	}
	return
}

// Store holds the embedded manifests keyed by Camunda line.
type Store struct{ byLine map[string]Manifest }

// LoadStore reads the manifests compiled into the binary.
func LoadStore() (*Store, error) { return loadStoreFS(manifestData, "data") }

func loadStoreFS(fsys embed.FS, dir string) (*Store, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{byLine: map[string]Manifest{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := fsys.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err := yaml.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parsing embedded manifest %s: %w", e.Name(), err)
		}
		if m.Line == "" {
			return nil, fmt.Errorf("embedded manifest %s has no line field", e.Name())
		}
		s.byLine[m.Line] = m
	}
	if len(s.byLine) == 0 {
		return nil, fmt.Errorf("this build embeds no migration data")
	}
	return s, nil
}

func (s *Store) Get(l Line) (Manifest, bool) {
	m, ok := s.byLine[l.String()]
	return m, ok
}

// Lines returns every line with embedded data, oldest first.
func (s *Store) Lines() []Line {
	var out []Line
	for k := range s.byLine {
		if l, err := ParseLine(k); err == nil {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// Latest is the newest line with embedded data; the default upgrade target.
func (s *Store) Latest() Line {
	lines := s.Lines()
	return lines[len(lines)-1]
}

// ByActionRemovedIn returns the deprecated keys, which are the only ones that carry a
// scheduled removal version.
func (m Manifest) ByActionRemovedIn() []Key {
	var out []Key
	for _, k := range m.Keys {
		if k.Action == ActionDeprecated {
			out = append(out, k)
		}
	}
	return out
}
