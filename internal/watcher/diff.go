package watcher

import "github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"

// Diff returns the subset of current findings whose Key was not present in the
// previous run's state — "new" here means "not already known", not "just started
// happening"; a finding present since before this tool was deployed will still
// report as new exactly once, on the first run, which is the correct behavior for
// a monitor that has no earlier history to compare against.
func Diff(current []rules.Finding, previousKeys map[string]bool) []rules.Finding {
	var out []rules.Finding
	for _, f := range current {
		if !previousKeys[Key(f)] {
			out = append(out, f)
		}
	}
	return out
}

// KeysOf extracts the Key of every finding, for saving as the next run's state.
func KeysOf(findings []rules.Finding) []string {
	keys := make([]string, 0, len(findings))
	for _, f := range findings {
		keys = append(keys, Key(f))
	}
	return keys
}
