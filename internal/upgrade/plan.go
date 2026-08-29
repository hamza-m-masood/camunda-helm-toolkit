package upgrade

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

// Upgrade findings use the CCD1xx range to keep them distinct from the CCD0xx
// misconfiguration checks while staying in one ID namespace.
const (
	RuleRemovedKey     = "CCD101"
	RuleRenamedKey     = "CCD102"
	RuleDeprecatedKey  = "CCD103"
	RuleBundledBitnami = "CCD104"
	RuleMinorSkip      = "CCD105"
	RuleEscapeHatch    = "CCD106"
	RuleHelmCLI        = "CCD107"
	RuleSupportEOL     = "CCD108"
	RuleNoData         = "CCD109"
)

// Request is everything the planner needs. It performs no I/O.
type Request struct {
	Release   string
	Namespace string
	// ChartVersion is the installed chart semver, for display only.
	ChartVersion string
	From         Line
	To           Line
	// UserValues must be the values the operator supplied, NOT the chart-defaults
	// merged view. Diagnosing against merged values reports every deprecated key in
	// the chart as if the user had set it.
	UserValues values.Values
	// StripRemoved drops removed keys from the migrated values. Renames are always
	// applied because the replacement is known; dropping a setting is a judgement
	// call about intent, so it stays opt-in.
	StripRemoved bool
}

// ValueChange records one edit the planner made to produce the migrated values.
type ValueChange struct {
	Action string `json:"action"`
	From   string `json:"from"`
	To     string `json:"to,omitempty"`
	Hop    string `json:"hop"`
}

// Plan is the result of planning an upgrade.
type Plan struct {
	Release      string   `json:"release,omitempty"`
	Namespace    string   `json:"namespace,omitempty"`
	ChartVersion string   `json:"chartVersion,omitempty"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Hops         []string `json:"hops"`
	// TargetChartVersion is the chart major for To, e.g. "15.x".
	TargetChartVersion string          `json:"targetChartVersion,omitempty"`
	Support            string          `json:"support,omitempty"`
	Findings           []rules.Finding `json:"findings"`
	ValueChanges       []ValueChange   `json:"valueChanges,omitempty"`
	Runbook            []Step          `json:"runbook,omitempty"`
	HopsWithSteps      map[string]bool `json:"hopsWithSteps,omitempty"`
	Facts              Facts           `json:"facts"`
	// MigratedValues is the rewritten values tree, serialised separately so the JSON
	// findings payload stays the same shape the check command emits.
	MigratedValues values.Values `json:"-"`
	// Approximate counts findings where the chart's condition could not be modelled
	// exactly, so the summary can be honest about precision.
	Approximate int `json:"approximate,omitempty"`
}

// Plan compares the supplied values against every line between From and To.
func (r Request) Plan(store *Store) (*Plan, error) {
	hops, err := Hops(r.From, r.To)
	if err != nil {
		return nil, err
	}

	p := &Plan{
		Release:        r.Release,
		Namespace:      r.Namespace,
		ChartVersion:   r.ChartVersion,
		From:           r.From.String(),
		To:             r.To.String(),
		MigratedValues: values.DeepCopy(r.UserValues),
	}
	if major, ok := ChartMajorForLine(r.To); ok {
		p.TargetChartVersion = fmt.Sprintf("%d.x", major)
	}
	if fromManifest, ok := store.Get(r.From); ok {
		p.Support = fromManifest.Support
	}
	if len(hops) == 0 {
		return p, nil
	}
	for _, h := range hops {
		p.Hops = append(p.Hops, h.String())
	}

	if len(hops) > 1 {
		p.Findings = append(p.Findings, rules.Finding{
			RuleID:   RuleMinorSkip,
			Severity: rules.High,
			Title:    fmt.Sprintf("%s to %s is %d minor upgrades, not one", r.From, r.To, len(hops)),
			Detail: "Camunda does not support skipping minor versions. Each hop is its own helm " +
				"upgrade and has to reach a healthy state before the next one starts. Findings below " +
				"are grouped by the hop that introduces them.",
			Remediation: "Plan the hops in order: " + r.From.String() + " -> " + strings.Join(p.Hops, " -> "),
		})
	}
	if p.Support == "endOfLife" {
		p.Findings = append(p.Findings, rules.Finding{
			RuleID:   RuleSupportEOL,
			Severity: rules.Low,
			Title:    fmt.Sprintf("%s is end-of-life", r.From),
			Detail: "Upgrading off an end-of-life line is the right move, but expect thinner " +
				"migration tooling and documentation coverage for the first hop.",
		})
	}

	for _, hop := range hops {
		man, ok := store.Get(hop)
		if !ok {
			p.Findings = append(p.Findings, rules.Finding{
				RuleID:   RuleNoData,
				Severity: rules.Medium,
				Title:    fmt.Sprintf("No migration data for the %s hop", hop),
				Detail: "This build cannot check values-key changes for that hop, so the findings " +
					"below are incomplete for it.",
				Remediation: "Check for a newer camunda-helm-toolkit release.",
			})
			continue
		}
		if man.RequiresHelmMajor > 0 {
			p.Findings = append(p.Findings, rules.Finding{
				RuleID:   RuleHelmCLI,
				Severity: rules.Medium,
				Title: fmt.Sprintf("Chart %d.x (%s) requires Helm CLI v%d or later",
					man.ChartMajor, hop, man.RequiresHelmMajor),
				Detail:      "The chart fails at render time on an older CLI, before anything is applied.",
				Remediation: "Check with: helm version --short",
			})
		}
		p.applyManifest(hop, man, r.StripRemoved)
	}

	p.Facts = detectFacts(r.UserValues)
	p.Findings = append(p.Findings, bundledBitnamiFindings(r, p.Facts)...)
	p.Findings = append(p.Findings, escapeHatchFindings(r.UserValues)...)

	for _, f := range p.Findings {
		if strings.Contains(f.Detail, approximateNote) {
			p.Approximate++
		}
	}

	steps, err := RunbookFor(p.Hops, p.Facts, Substitutions{
		Release:            r.Release,
		Namespace:          r.Namespace,
		TargetChartVersion: p.TargetChartVersion,
	})
	if err != nil {
		return nil, err
	}
	p.Runbook = steps
	if p.HopsWithSteps, err = HopsWithSteps(p.Hops); err != nil {
		return nil, err
	}
	return p, nil
}

const approximateNote = "The chart's own condition for this key is too complex to evaluate " +
	"exactly, so this is reported on key presence alone and may be a false alarm."

func (p *Plan) applyManifest(hop Line, man Manifest, stripRemoved bool) {
	for _, k := range man.Keys {
		hit, approx := tripped(p.MigratedValues, k)
		if !hit {
			continue
		}
		f := rules.Finding{Path: k.Old}
		switch k.Action {
		case ActionRenamed:
			f.RuleID = RuleRenamedKey
			f.Severity = rules.High
			f.Title = fmt.Sprintf("[%s] %s was renamed to %s", hop, k.Old, k.New)
			f.Detail = "The chart fails to render while the old key is set."
			f.Remediation = fmt.Sprintf("Rename to %s. Already applied in the migrated values.", k.New)
			p.applyRename(hop, k, &f)
		case ActionRemoved:
			f.RuleID = RuleRemovedKey
			f.Severity = rules.High
			f.Title = fmt.Sprintf("[%s] %s was removed", hop, k.Old)
			f.Detail = "The chart fails to render while this key is set."
			f.Remediation = replacementAdvice(k)
			if stripRemoved && values.DeletePath(p.MigratedValues, k.OldPrefix()) {
				p.ValueChanges = append(p.ValueChanges, ValueChange{
					Action: "removed", From: k.Old, Hop: hop.String(),
				})
			}
		case ActionDeprecated:
			f.RuleID = RuleDeprecatedKey
			f.Severity = rules.Low
			f.Title = fmt.Sprintf("[%s] %s is deprecated", hop, k.Old)
			if k.RemovedIn != "" {
				f.Title += fmt.Sprintf(", scheduled for removal in chart v%s", k.RemovedIn)
			}
			f.Detail = "The upgrade still succeeds; this is a warning the chart will keep emitting."
			f.Remediation = replacementAdvice(k)
		}
		if approx {
			f.Detail = strings.TrimSpace(f.Detail + " " + approximateNote)
		}
		if k.Source != "" {
			f.Detail = strings.TrimSpace(f.Detail) + " (chart source: " + k.Source + ")"
		}
		p.Findings = append(p.Findings, f)
	}
}

// applyRename rewrites the migrated values for one rename. A subtree rename moves every
// leaf under the old prefix; a leaf rename moves one value. Conflicts are surfaced in the
// finding rather than resolved, because picking a winner would change behaviour silently.
func (p *Plan) applyRename(hop Line, k Key, f *rules.Finding) {
	if k.New == "" {
		return
	}
	if k.IsSubtree() {
		moved, conflicts := values.MoveSubtree(p.MigratedValues, k.OldPrefix(), k.NewPrefix())
		if len(moved) == 0 && len(conflicts) == 0 {
			return
		}
		p.ValueChanges = append(p.ValueChanges, ValueChange{
			Action: fmt.Sprintf("renamed subtree (%d key(s))", len(moved)),
			From:   k.OldPrefix(), To: k.NewPrefix(), Hop: hop.String(),
		})
		if len(conflicts) > 0 {
			f.Severity = rules.High
			f.Remediation = fmt.Sprintf(
				"Move everything under %s to %s. %d destination key(s) already exist and were "+
					"NOT overwritten -- decide which value wins for: %s",
				k.OldPrefix(), k.NewPrefix(), len(conflicts), strings.Join(conflicts, ", "))
		}
		return
	}
	v, ok := values.GetPath(p.MigratedValues, k.Old)
	if !ok {
		return
	}
	if _, exists := values.GetPath(p.MigratedValues, k.New); exists {
		f.Remediation = fmt.Sprintf(
			"Both %s and %s are set. The new key was left as-is and the old one removed; "+
				"confirm the surviving value is the one you want.", k.Old, k.New)
		values.DeletePath(p.MigratedValues, k.Old)
		p.ValueChanges = append(p.ValueChanges, ValueChange{
			Action: "removed (superseded)", From: k.Old, To: k.New, Hop: hop.String(),
		})
		return
	}
	values.SetPath(p.MigratedValues, k.New, v)
	values.DeletePath(p.MigratedValues, k.Old)
	p.ValueChanges = append(p.ValueChanges, ValueChange{
		Action: "renamed", From: k.Old, To: k.New, Hop: hop.String(),
	})
}

func replacementAdvice(k Key) string {
	switch {
	case k.New != "":
		return "Use " + k.New + " instead."
	case k.Migration != "":
		return "Configure this via " + k.Migration + " instead."
	default:
		return "Remove the key; see https://docs.camunda.io/docs/self-managed/deployment/helm/upgrade/"
	}
}

// tripped mirrors the chart's own condition for a key. approx is true when the
// condition could not be classified and presence was used as a stand-in.
func tripped(v values.Values, k Key) (hit, approx bool) {
	lookup := k.Old
	if k.IsSubtree() {
		// A subtree key is tripped when the subtree exists and holds anything; the
		// literal path with the "*" in it never exists.
		lookup = k.OldPrefix()
		raw, present := values.GetPath(v, lookup)
		return present && !isEmpty(raw), false
	}
	raw, present := values.GetPath(v, lookup)
	if !present {
		return false, false
	}
	switch k.Trigger {
	case TriggerPresence:
		return true, false
	case TriggerNotDefault:
		if k.Default == "" {
			return true, true
		}
		return fmt.Sprint(raw) != k.Default, false
	case TriggerNonEmpty:
		return !isEmpty(raw), false
	case TriggerTruthy:
		return isTruthy(raw), false
	case TriggerFalsy:
		return !isTruthy(raw), false
	default:
		return true, true
	}
}

func isEmpty(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	default:
		return false
	}
}

func isTruthy(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "false"
	case int:
		return t != 0
	case float64:
		return t != 0
	case []interface{}:
		return len(t) > 0
	case map[string]interface{}:
		return len(t) > 0
	default:
		return true
	}
}

// bundledSubcharts maps the values key that enables a bundled Bitnami dependency to the
// name used in findings.
var bundledSubcharts = map[string]string{
	"identityKeycloak.enabled":     "Keycloak",
	"identityPostgresql.enabled":   "Identity PostgreSQL",
	"postgresql.enabled":           "PostgreSQL",
	"webModelerPostgresql.enabled": "Web Modeler PostgreSQL",
	"elasticsearch.enabled":        "Elasticsearch",
}

func detectFacts(v values.Values) Facts {
	on := func(path string) bool {
		raw, ok := values.GetPath(v, path)
		return ok && isTruthy(raw)
	}
	return Facts{
		BundledKeycloak: on("identityKeycloak.enabled"),
		BundledPostgres: on("identityPostgresql.enabled") || on("postgresql.enabled") ||
			on("webModelerPostgresql.enabled"),
		BundledElasticsearch: on("elasticsearch.enabled"),
	}
}

// line810 is the release that drops the bundled Bitnami subcharts.
var line810 = Line{8, 10}

func bundledBitnamiFindings(r Request, facts Facts) []rules.Finding {
	if r.To.Compare(line810) < 0 || (!facts.BundledKeycloak && !facts.BundledPostgres) {
		return nil
	}
	var which []string
	for path, name := range bundledSubcharts {
		if raw, ok := values.GetPath(r.UserValues, path); ok && isTruthy(raw) {
			which = append(which, name)
		}
	}
	sort.Strings(which)
	return []rules.Finding{{
		RuleID:   RuleBundledBitnami,
		Severity: rules.Critical,
		Title:    "Bundled Bitnami subcharts are enabled and 8.10 removes them",
		Detail: "Enabled: " + strings.Join(which, ", ") + ". Chart 15.x no longer ships these " +
			"subcharts. The Keycloak realm and the Identity / Web Modeler databases live inside " +
			"them, so upgrading without moving that data first removes the workloads holding it. " +
			"This is a data migration between two Helm operations, not a values change.",
		Remediation: "Follow runbook step bitnami-offload before the upgrade.",
	}}
}

// escapeHatchFields are the values keys where operators park raw application config.
// The chart cannot validate their contents, so they are invisible to every other check.
var escapeHatchFields = []string{"env", "extraConfiguration", "command", "extraVolumeMounts"}

var escapeHatchComponents = []string{
	"orchestration", "identity", "optimize", "connectors", "console",
	"webModeler", "webModeler.restapi", "camundaHub", "camundaHub.restapi",
	"zeebe", "zeebeGateway", "operate", "tasklist",
}

func escapeHatchFindings(v values.Values) []rules.Finding {
	var out []rules.Finding
	for _, comp := range escapeHatchComponents {
		for _, field := range escapeHatchFields {
			path := comp + "." + field
			raw, ok := values.GetPath(v, path)
			if !ok || isEmpty(raw) {
				continue
			}
			out = append(out, rules.Finding{
				RuleID:   RuleEscapeHatch,
				Severity: rules.Medium,
				Title:    fmt.Sprintf("%s is set (%s)", path, describeSize(raw)),
				Path:     path,
				Detail: "The chart does not validate this field, so it is invisible to every other " +
					"check here. If any entry works around a chart limitation that the target version " +
					"fixes internally, the override can conflict with the new behaviour after upgrading.",
				Remediation: "Review each entry against the target version's defaults before upgrading.",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func describeSize(v interface{}) string {
	n := -1
	switch t := v.(type) {
	case []interface{}:
		n = len(t)
	case map[string]interface{}:
		n = len(t)
	}
	if n < 0 {
		return "set"
	}
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}
