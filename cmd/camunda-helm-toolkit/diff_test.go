package main

import (
	"strings"
	"testing"
)

func TestDiffBinary_RequiresBothReleases(t *testing.T) {
	out, err := runBinary("diff", "--release-a", "foo")
	if err == nil {
		t.Fatal("expected a nonzero exit when --release-b is missing")
	}
	if !strings.Contains(out, "--release-a and --release-b are both required") {
		t.Errorf("expected a clear missing-flag error, got:\n%s", out)
	}
}

func TestDiffBinary_NonexistentReleaseFailsClearly(t *testing.T) {
	out, err := runBinary("diff", "--release-a", "this-release-does-not-exist-anywhere", "--release-b", "neither-does-this-one")
	if err == nil {
		t.Fatal("expected a nonzero exit for a nonexistent release")
	}
	if !strings.Contains(out, "error: fetching") {
		t.Errorf("expected a clear fetch error, got:\n%s", out)
	}
}
