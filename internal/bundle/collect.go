package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/live"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

func kubectlBin() string {
	if _, err := exec.LookPath("kubectl"); err == nil {
		return "kubectl"
	}
	return "oc"
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	// Bundle collection is best-effort per-file: a missing PDB or a permissions
	// gap on one describe call shouldn't abort the whole bundle. Callers decide
	// whether to include the error text as the file's content (still useful —
	// it tells the support engineer what couldn't be collected and why) or skip.
	if err != nil {
		return out.String() + stderr.String(), err
	}
	return out.String(), nil
}

// File is one entry that will go into the bundle archive.
type File struct {
	Name    string // path within the archive
	Content []byte
	Error   error // non-nil if collection failed; Content may still hold partial/error text
}

// Manifest is written to manifest.json at the archive root, listing every file
// actually included — so a customer can see exactly what they're about to send
// before attaching the bundle to a ticket, and again after extracting it.
type Manifest struct {
	Release   string            `json:"release"`
	Namespace string            `json:"namespace"`
	Files     []string          `json:"files"`
	Errors    map[string]string `json:"errors,omitempty"`
}

// Plan describes what Collect will gather, without gathering it — used for
// --dry-run so an operator can review the list before anything touches the
// cluster or disk.
func Plan(release, namespace string) []string {
	return []string{
		"findings.json",
		"values-redacted.yaml",
		"describe-statefulset.txt",
		"describe-pods.txt",
		"describe-pdb.txt",
		"events.txt",
		"logs/",
		"helm-list.txt",
		"versions.txt",
		"manifest.json",
	}
}

var nodeIdentifyingFieldRe = regexp.MustCompile(`(?im)^(\s*(name|nodeName|hostIP|podIP|internalIP|externalIP)\s*:\s*).*$`)

// redactInfra strips node/pod identifying details (names, IPs) from text this
// package didn't structure itself (kubectl describe/get -o wide output is plain
// text, not something RedactValues' map-walk can operate on).
func redactInfra(s string) string {
	return nodeIdentifyingFieldRe.ReplaceAllString(s, "${1}<redacted>")
}

// Collect gathers everything Plan lists. findingsJSON is the already-computed
// `check --live` output (the caller runs the normal check pipeline so this package
// doesn't duplicate that logic). errs collects non-fatal per-file failures.
func Collect(release, namespace string, findingsJSON []byte) ([]File, error) {
	var files []File
	errs := map[string]string{}

	add := func(name string, content string, err error) {
		if err != nil {
			errs[name] = err.Error()
		}
		files = append(files, File{Name: name, Content: []byte(content), Error: err})
	}

	files = append(files, File{Name: "findings.json", Content: findingsJSON})

	rawValues, err := live.GetHelmValues(namespace, release)
	if err != nil {
		add("values-redacted.yaml", fmt.Sprintf("error: %v\n", err), err)
	} else {
		v, perr := values.ParseYAML(rawValues)
		if perr != nil {
			add("values-redacted.yaml", fmt.Sprintf("error parsing values: %v\n", perr), perr)
		} else {
			redacted := RedactValues(map[string]interface{}(v))
			out, merr := yaml.Marshal(redacted)
			if merr != nil {
				add("values-redacted.yaml", fmt.Sprintf("error marshaling redacted values: %v\n", merr), merr)
			} else {
				add("values-redacted.yaml", string(out), nil)
			}
		}
	}

	kc := kubectlBin()
	sel := fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=zeebe-broker", release)

	out, err := run(kc, "describe", "statefulset", "-n", namespace, "-l", sel)
	add("describe-statefulset.txt", redactInfra(out), err)

	out, err = run(kc, "describe", "pods", "-n", namespace, "-l", sel)
	add("describe-pods.txt", redactInfra(out), err)

	out, err = run(kc, "describe", "pdb", "-n", namespace, "-l", sel)
	add("describe-pdb.txt", redactInfra(out), err)

	out, err = run(kc, "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
	add("events.txt", redactInfra(out), err)

	out, err = run(kc, "logs", "-n", namespace, "-l", sel, "--tail=200", "--all-containers", "--prefix")
	add("logs/current.txt", out, err)
	out, err = run(kc, "logs", "-n", namespace, "-l", sel, "--tail=200", "--all-containers", "--prefix", "--previous")
	add("logs/previous.txt", out, err) // expected to error when nothing has crashed — that's fine, not a real failure

	out, err = run("helm", "list", "-n", namespace)
	add("helm-list.txt", out, err)

	files = append(files, File{Name: "versions.txt", Content: []byte(collectVersions(kc, release, namespace))})

	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	manifest := Manifest{Release: release, Namespace: namespace, Files: names, Errors: errs}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	files = append(files, File{Name: "manifest.json", Content: manifestJSON})

	return files, nil
}

func collectVersions(kc, release, namespace string) string {
	var b strings.Builder
	if out, err := run(kc, "version", "-o", "json"); err == nil {
		fmt.Fprintln(&b, "--- kubectl/server version ---")
		fmt.Fprintln(&b, out)
	}
	if out, err := run("helm", "version", "--short"); err == nil {
		fmt.Fprintln(&b, "--- helm version ---")
		fmt.Fprintln(&b, out)
	}
	if out, err := run(kc, "get", "nodes", "-o", "custom-columns=VERSION:.status.nodeInfo.kubeletVersion", "--no-headers"); err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		fmt.Fprintf(&b, "--- node count and kubelet versions (names/IPs omitted) ---\ncount: %d\n%s\n", len(lines), out)
	}
	if out, err := run("helm", "get", "metadata", release, "-n", namespace, "-o", "json"); err == nil {
		fmt.Fprintln(&b, "--- chart/app version (helm get metadata) ---")
		fmt.Fprintln(&b, out)
	}
	return b.String()
}
