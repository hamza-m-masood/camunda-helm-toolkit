package main

import (
	"os/exec"
	"strings"
	"testing"
)

// An adversarial audit found that --retention-days 0 (or a negative value) was
// silently replaced by the default of 7 with no indication anything had been
// overridden — capacityplan.Recommend treats any RetentionDays <= 0 as "the caller
// didn't specify one", which is correct for a caller that never sets the field, but
// wrong for a user who explicitly typed a bad value on this CLI. These tests build and
// exec the actual binary so this exact silent-substitution gap cannot reopen quietly.
func TestSizeBinary_RetentionDays(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantErr    bool
		wantOutput string // substring expected in combined output
	}{
		{
			name:    "explicit zero is rejected",
			args:    []string{"size", "--throughput", "100", "--avg-payload-kb", "2", "--retention-days", "0"},
			wantErr: true,
		},
		{
			name:    "explicit negative is rejected",
			args:    []string{"size", "--throughput", "100", "--avg-payload-kb", "2", "--retention-days", "-5"},
			wantErr: true,
		},
		{
			name:       "omitted entirely still defaults to 7",
			args:       []string{"size", "--throughput", "100", "--avg-payload-kb", "2"},
			wantErr:    false,
			wantOutput: "--retention-days (7)",
		},
		{
			name:       "explicit valid value is honored, not silently replaced",
			args:       []string{"size", "--throughput", "100", "--avg-payload-kb", "2", "--retention-days", "30"},
			wantErr:    false,
			wantOutput: "--retention-days (30)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runBinary(tc.args...)
			var exitErr *exec.ExitError
			gotErr := false
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					exitErr = ee
					gotErr = true
				} else {
					t.Fatalf("unexpected non-exit error running binary: %v", err)
				}
			}
			if gotErr != tc.wantErr {
				t.Fatalf("args %v: wantErr=%v gotErr=%v (exit=%v), output:\n%s", tc.args, tc.wantErr, gotErr, exitErr, out)
			}
			if tc.wantOutput != "" && !strings.Contains(out, tc.wantOutput) {
				t.Errorf("args %v: expected output to contain %q, got:\n%s", tc.args, tc.wantOutput, out)
			}
		})
	}
}
