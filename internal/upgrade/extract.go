package upgrade

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The chart already encodes every values-key change as a call to one of three helpers
// in templates/**/constraints.tpl — camundaPlatform.keyRemoved, keyRenamed and
// keyDeprecated. Those call sites are the chart's own source of truth: they are what
// actually fails or warns at render time. Generating from them means this tool cannot
// disagree with the chart about what changed, and a new deprecation shows up here as
// soon as someone adds the guard, with no second list to remember to update.

var helperActions = map[string]Action{
	"camundaPlatform.keyRemoved":    ActionRemoved,
	"camundaPlatform.keyRenamed":    ActionRenamed,
	"camundaPlatform.keyDeprecated": ActionDeprecated,
}

var (
	includeRe   = regexp.MustCompile(`include\s+"(camundaPlatform\.key(?:Removed|Renamed|Deprecated))"`)
	symbolRe    = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\s*:=\s*"([^"]*)"`)
	helmMajorRe = regexp.MustCompile(`requires Helm CLI v(\d+)`)
	removedInRe = regexp.MustCompile(`removed in chart v(\d+)`)
	neDefaultRe = regexp.MustCompile(`^ne\s+\(.*\)\s+"([^"]*)"$`)
	chartMajRe  = regexp.MustCompile(`(?m)^version:\s*v?(\d+)\.`)
)

// Extract builds the manifest for one Camunda line from a camunda-platform-helm checkout.
func Extract(chartRepo string, line Line) (Manifest, error) {
	chartDir := filepath.Join(chartRepo, "charts", "camunda-platform-"+line.String())
	if _, err := os.Stat(chartDir); err != nil {
		return Manifest{}, fmt.Errorf("chart for %s not found under %s: %w", line, chartRepo, err)
	}
	files, err := filepath.Glob(filepath.Join(chartDir, "templates", "*", "constraints.tpl"))
	if err != nil {
		return Manifest{}, err
	}
	// Older lines keep constraints alongside the other camunda templates rather than
	// in templates/common, so fall back to a recursive search before giving up.
	if len(files) == 0 {
		err = filepath.WalkDir(chartDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && d.Name() == "constraints.tpl" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return Manifest{}, err
		}
	}
	if len(files) == 0 {
		return Manifest{}, fmt.Errorf("no constraints.tpl found under %s", chartDir)
	}
	sort.Strings(files)

	m := Manifest{Line: line.String()}
	m.ChartMajor = chartMajorFromChartYAML(chartDir)
	if m.ChartMajor == 0 {
		if major, ok := ChartMajorForLine(line); ok {
			m.ChartMajor = major
		}
	}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return Manifest{}, err
		}
		text := string(raw)
		rel, relErr := filepath.Rel(chartRepo, f)
		if relErr != nil {
			rel = f
		}

		if mm := helmMajorRe.FindStringSubmatch(text); mm != nil && m.RequiresHelmMajor == 0 {
			m.RequiresHelmMajor, _ = strconv.Atoi(mm[1])
		}
		removedIn := ""
		if mm := removedInRe.FindStringSubmatch(text); mm != nil {
			removedIn = mm[1]
		}

		// The helper definitions carry "Usage:" examples inside {{/* ... */}} blocks,
		// which are byte-identical to real call sites. Blanking comment bodies while
		// preserving newlines keeps those examples out of the data without throwing
		// off the source line numbers.
		stripped := blankComments(text)
		keys, err := scanCallSites(stripped, symbolTable(stripped), rel, removedIn)
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", rel, err)
		}
		m.Keys = append(m.Keys, keys...)
	}

	m.Keys = dedupe(m.Keys)
	sort.SliceStable(m.Keys, func(i, j int) bool {
		if m.Keys[i].Action != m.Keys[j].Action {
			return m.Keys[i].Action < m.Keys[j].Action
		}
		return m.Keys[i].Old < m.Keys[j].Old
	})
	m.Support = supportBucket(chartRepo, line)
	m.GeneratedFrom = gitSHA(chartRepo)
	return m, nil
}

func chartMajorFromChartYAML(chartDir string) int {
	raw, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		return 0
	}
	if m := chartMajRe.FindStringSubmatch(string(raw)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// blankComments overwrites the body of every {{/* ... */}} block with spaces, leaving
// newlines in place so reported line numbers still match the original file.
func blankComments(s string) string {
	out := []byte(s)
	for i := 0; i < len(out); {
		if !strings.HasPrefix(s[i:], "{{/*") && !strings.HasPrefix(s[i:], "{{- /*") {
			i++
			continue
		}
		end := strings.Index(s[i:], "*/}}")
		if end < 0 {
			break
		}
		for j := i; j < i+end+4 && j < len(out); j++ {
			if out[j] != '\n' {
				out[j] = ' '
			}
		}
		i += end + 4
	}
	return string(out)
}

type symbols map[string]string

// resolve expands a $variable reference to the string literal it was assigned, which is
// how the chart shares migration targets like "orchestration.extraConfiguration" across
// dozens of call sites.
func (t symbols) resolve(v string) string {
	if strings.HasPrefix(v, "$") {
		if lit, ok := t[strings.TrimPrefix(v, "$")]; ok {
			return lit
		}
	}
	return v
}

func symbolTable(s string) symbols {
	tbl := symbols{}
	for _, m := range symbolRe.FindAllStringSubmatch(s, -1) {
		tbl[m[1]] = m[2]
	}
	return tbl
}

func scanCallSites(text string, tbl symbols, sourceFile, removedIn string) ([]Key, error) {
	var out []Key
	for _, loc := range includeRe.FindAllStringSubmatchIndex(text, -1) {
		action, ok := helperActions[text[loc[2]:loc[3]]]
		if !ok {
			continue
		}
		rest := text[loc[1]:]
		open := strings.Index(rest, "(")
		if open < 0 {
			continue
		}
		args, ok := balanced(rest[open:])
		if !ok {
			return nil, fmt.Errorf("unbalanced dict arguments at line %d", lineOf(text, loc[0]))
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(args, "(")), "dict") {
			continue
		}
		k := Key{
			Action:    action,
			Old:       tbl.resolve(argValue(args, "oldName")),
			New:       tbl.resolve(argValue(args, "newName")),
			Migration: tbl.resolve(argValue(args, "migration")),
			URL:       tbl.resolve(argValue(args, "url")),
			Condition: normalizeCondition(argValue(args, "condition")),
			Source:    fmt.Sprintf("%s:%d", sourceFile, lineOf(text, loc[0])),
		}
		if k.Old == "" {
			continue
		}
		if action == ActionDeprecated {
			k.RemovedIn = removedIn
		}
		k.Trigger, k.Default = classify(k.Condition)
		out = append(out, k)
	}
	return out, nil
}

// balanced returns s[0:] through the paren matching s[0]=='(', ignoring parens that
// appear inside double-quoted strings.
func balanced(s string) (string, bool) {
	depth := 0
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if inStr {
				i++
			}
		case '"':
			inStr = !inStr
		case '(':
			if !inStr {
				depth++
			}
		case ')':
			if !inStr {
				depth--
				if depth == 0 {
					return s[:i+1], true
				}
			}
		}
	}
	return "", false
}

// argValue pulls one named argument out of a Helm `dict` literal. A value is a quoted
// string, a parenthesised expression, or a bare token such as a $variable.
func argValue(args, name string) string {
	idx := strings.Index(args, `"`+name+`"`)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimLeft(args[idx+len(name)+2:], " \t\r\n")
	if rest == "" {
		return ""
	}
	switch rest[0] {
	case '"':
		end := closingQuote(rest)
		if end < 0 {
			return ""
		}
		if v, err := strconv.Unquote(rest[:end]); err == nil {
			return v
		}
		return strings.Trim(rest[:end], `"`)
	case '(':
		if b, ok := balanced(rest); ok {
			return b
		}
		return ""
	default:
		end := strings.IndexAny(rest, " \t\r\n)")
		if end < 0 {
			end = len(rest)
		}
		return rest[:end]
	}
}

func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return -1
}

// normalizeCondition collapses whitespace and strips the single enclosing paren pair the
// dict literal adds, so `(.Values.identity.keycloak)` reads as `.Values.identity.keycloak`.
func normalizeCondition(c string) string {
	c = strings.Join(strings.Fields(c), " ")
	for len(c) > 1 && c[0] == '(' && c[len(c)-1] == ')' {
		if b, ok := balanced(c); !ok || len(b) != len(c) {
			break
		}
		c = strings.TrimSpace(c[1 : len(c)-1])
	}
	return c
}

func classify(cond string) (Trigger, string) {
	switch {
	case cond == "":
		return TriggerUnknown, ""
	case strings.HasPrefix(cond, "and "), strings.Contains(cond, " and "),
		strings.HasPrefix(cond, "or "), strings.Contains(cond, " or "):
		return TriggerUnknown, ""
	case strings.Contains(cond, "hasKey"):
		return TriggerPresence, ""
	// `ne nil (dig "a" "b" nil $values)` asks whether a subtree exists at all, which is
	// the same question hasKey asks, just spelled for nested paths.
	case strings.HasPrefix(cond, "ne nil (dig ") || strings.HasPrefix(cond, "ne nil (dig\t"):
		return TriggerPresence, ""
	case strings.HasPrefix(cond, "ne "):
		if m := neDefaultRe.FindStringSubmatch(cond); m != nil {
			return TriggerNotDefault, m[1]
		}
		return TriggerNotDefault, ""
	case strings.HasPrefix(cond, "not (empty"), strings.HasPrefix(cond, "not empty"):
		return TriggerNonEmpty, ""
	case strings.HasPrefix(cond, "not "):
		return TriggerFalsy, ""
	case strings.HasPrefix(cond, ".Values."):
		return TriggerTruthy, ""
	default:
		return TriggerUnknown, ""
	}
}

// dedupe keeps the first entry per (action, key); a key is occasionally guarded from
// more than one template.
func dedupe(keys []Key) []Key {
	seen := map[string]bool{}
	out := make([]Key, 0, len(keys))
	for _, k := range keys {
		id := string(k.Action) + "|" + k.Old
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, k)
	}
	return out
}

func lineOf(s string, off int) int { return strings.Count(s[:off], "\n") + 1 }

func supportBucket(chartRepo string, line Line) string {
	raw, err := os.ReadFile(filepath.Join(chartRepo, "charts", "chart-versions.yaml"))
	if err != nil {
		return ""
	}
	var doc struct {
		CamundaVersions map[string][]string `yaml:"camundaVersions"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return ""
	}
	for bucket, lines := range doc.CamundaVersions {
		for _, l := range lines {
			if l == line.String() {
				return bucket
			}
		}
	}
	return ""
}

func gitSHA(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
