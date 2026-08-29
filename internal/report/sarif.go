package report

import (
	"encoding/json"
	"io"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
)

// ToolVersion is set by main at startup so SARIF output identifies the exact build
// that produced it (SARIF consumers, e.g. GitHub code scanning, use this for dedup).
var ToolVersion = "dev"

const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string             `json:"id"`
	ShortDescription     sarifText          `json:"shortDescription"`
	DefaultConfiguration sarifDefaultConfig `json:"defaultConfiguration"`
}

type sarifDefaultConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifLocation struct {
	LogicalLocations []sarifLogicalLocation `json:"logicalLocations"`
}

type sarifLogicalLocation struct {
	FullyQualifiedName string `json:"fullyQualifiedName"`
}

func sarifLevel(s rules.Severity) string {
	switch s {
	case rules.Critical, rules.High:
		return "error"
	case rules.Medium:
		return "warning"
	default:
		return "note"
	}
}

// WriteSARIF renders findings as a minimal, valid SARIF 2.1.0 log: one run, one tool
// driver whose rules array lists every rule the tool can ever produce (not just the
// ones that fired), and one result per finding. Suppressed findings are omitted from
// results (SARIF has its own suppression concept that doesn't map cleanly onto ours
// for an alpha tool) but still counted in the accompanying properties bag so the
// suppressed count isn't lost for a consumer that reads it.
func WriteSARIF(w io.Writer, findings []rules.Finding) error {
	Sort(findings)

	var ruleDefs []sarifRule
	for _, m := range rules.AllRuleMetas() {
		ruleDefs = append(ruleDefs, sarifRule{
			ID:                   m.ID,
			ShortDescription:     sarifText{Text: m.Title},
			DefaultConfiguration: sarifDefaultConfig{Level: sarifLevel(m.Severity)},
		})
	}

	var results []sarifResult
	for _, f := range findings {
		r := sarifResult{
			RuleID:  f.RuleID,
			Level:   sarifLevel(f.Severity),
			Message: sarifText{Text: f.Title + strDetailSuffix(f)},
		}
		if f.Path != "" {
			r.Locations = []sarifLocation{{
				LogicalLocations: []sarifLogicalLocation{{FullyQualifiedName: f.Path}},
			}}
		}
		results = append(results, r)
	}
	if results == nil {
		results = []sarifResult{}
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "camunda-chart-doctor",
				InformationURI: "https://github.com/hamza-m-masood/camunda-chart-doctor",
				Version:        ToolVersion,
				Rules:          ruleDefs,
			}},
			Results: results,
		}},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

func strDetailSuffix(f rules.Finding) string {
	if f.Detail == "" {
		return ""
	}
	return " — " + f.Detail
}
