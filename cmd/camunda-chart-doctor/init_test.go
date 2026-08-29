package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These tests exist because internal/wizard's own self-check tests only ever call
// rules.AllValuesChecks() -- the wizard package itself never renders manifests, so
// CCD009/CCD010/CCD015 (all manifest-based) had zero automated coverage despite two of
// them already being documented exceptions. A manual CLI smoke test caught a real gap
// here (CCD010 firing on `init --auth-method basic` for any real, space-free password)
// only by accident: the specific password used in that manual run happened to contain
// spaces, which the CCD010 detection regex -- by design -- does not match. These tests
// build and exec the actual compiled binary, the same way a user would run it, so this
// exact class of gap cannot reopen silently again.

const (
	realChart89  = "/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.9"
	realChart810 = "/Users/hamza.masood/projects/camunda/self-managed/camunda-platform-helm/charts/camunda-platform-8.10"
)

var testBinary string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "ccd-binary-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	testBinary = filepath.Join(tmpDir, "camunda-chart-doctor")
	build := exec.Command("go", "build", "-o", testBinary, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building the binary under test: " + err.Error())
	}

	os.Exit(m.Run())
}

func runBinary(args ...string) (string, error) {
	cmd := exec.Command(testBinary, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func skipIfChartMissing(t *testing.T, chart string) {
	t.Helper()
	if _, err := os.Stat(chart); err != nil {
		t.Skipf("real chart checkout not available at %s: %v", chart, err)
	}
}

// TestInitBinary_BasicAuth_RealChart is the direct regression test for the reported
// gap: a realistic, space-free password (the shape almost every real password takes)
// must not fail init's own self-check.
func TestInitBinary_BasicAuth_RealChart(t *testing.T) {
	for _, chart := range []string{realChart89, realChart810} {
		t.Run(chart, func(t *testing.T) {
			skipIfChartMissing(t, chart)
			out, err := runBinary("init", "--non-interactive", "--chart", chart,
				"--release-name", "regressiontest",
				"--secondary-storage", "elasticsearch",
				"--auth-method", "basic",
				"--admin-username", "admin",
				"--admin-password", "a-real-password-123",
			)
			if err != nil {
				t.Fatalf("init failed against the real binary: %v\noutput:\n%s", err, out)
			}
		})
	}
}

// TestInitBinary_BasicAuthWithConnectors_RealChart covers a second, distinct
// propagation of the same underlying admin credential: enabling Connectors makes the
// chart's own template pull `first(orchestration.security.initialization.users)` into
// a SECOND ConfigMap (<release>-connectors-configuration), not just
// <release>-zeebe-configuration -- confirmed by reading both template sources
// (templates/orchestration/files/_application.yaml and
// templates/connectors/files/_application.yaml) before this test was written.
func TestInitBinary_BasicAuthWithConnectors_RealChart(t *testing.T) {
	for _, chart := range []string{realChart89, realChart810} {
		t.Run(chart, func(t *testing.T) {
			skipIfChartMissing(t, chart)
			out, err := runBinary("init", "--non-interactive", "--chart", chart,
				"--release-name", "regressiontest",
				"--secondary-storage", "elasticsearch",
				"--enable", "connectors",
				"--auth-method", "basic",
				"--admin-username", "admin",
				"--admin-password", "another-real-password-456",
			)
			if err != nil {
				t.Fatalf("init failed against the real binary: %v\noutput:\n%s", err, out)
			}
		})
	}
}

// TestInitBinary_EveryComponentEnabled_RealChart is the heaviest realistic combination
// this tool supports, run through the actual binary end to end (not the wizard package
// directly) -- the same combination validated by hand for the original PR, now kept as
// a permanent regression rather than a one-off manual run.
func TestInitBinary_EveryComponentEnabled_RealChart(t *testing.T) {
	for _, chart := range []string{realChart89, realChart810} {
		t.Run(chart, func(t *testing.T) {
			skipIfChartMissing(t, chart)
			out, err := runBinary("init", "--non-interactive", "--chart", chart,
				"--release-name", "regressiontest",
				"--secondary-storage", "opensearch",
				"--enable", "identity", "--enable", "connectors",
				"--enable", "optimize", "--enable", "webModeler",
				"--web-modeler-from-email", "camunda@example.invalid",
				"--auth-method", "basic",
				"--admin-username", "admin",
				"--admin-password", "yet-another-real-password-789",
				"--throughput", "800", "--avg-payload-kb", "3",
				"--ingress-host", "camunda.example.invalid", "--ingress-tls",
			)
			if err != nil {
				t.Fatalf("init failed against the real binary: %v\noutput:\n%s", err, out)
			}
		})
	}
}

func TestInitBinary_OIDC_RealChart(t *testing.T) {
	for _, chart := range []string{realChart89, realChart810} {
		t.Run(chart, func(t *testing.T) {
			skipIfChartMissing(t, chart)
			out, err := runBinary("init", "--non-interactive", "--chart", chart,
				"--release-name", "regressiontest",
				"--secondary-storage", "opensearch",
				"--auth-method", "oidc",
				"--oidc-issuer-url", "https://issuer.example.invalid/realms/camunda",
			)
			if err != nil {
				t.Fatalf("init failed against the real binary: %v\noutput:\n%s", err, out)
			}
		})
	}
}
