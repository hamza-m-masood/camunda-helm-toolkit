// Package live queries an actual Kubernetes/OpenShift cluster (via kubectl or oc,
// whichever is on PATH) to catch drift that values.yaml alone can never show: an
// existingSecret that was renamed after install, a PodDisruptionBudget that was never
// actually created despite values.yaml claiming it is enabled, and so on.
package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

func kubectlBin() string {
	if _, err := exec.LookPath("kubectl"); err == nil {
		return "kubectl"
	}
	return "oc"
}

func runJSON(args []string, out interface{}) error {
	bin := kubectlBin()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", bin, args, err, stderr.String())
	}
	return json.Unmarshal(stdout.Bytes(), out)
}

// GetHelmValues returns the effective (merged) values Helm recorded for an installed
// release — this already reflects Helm's own chart-defaults + overlay merge, so no
// separate merge step is needed for an installed-release check.
func GetHelmValues(namespace, release string) ([]byte, error) {
	cmd := exec.Command("helm", "get", "values", "-a", "-n", namespace, release, "-o", "yaml")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm get values failed: %w: %s", err, stderr.String())
	}
	return out.Bytes(), nil
}

type pdbList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			DisruptionsAllowed int `json:"disruptionsAllowed"`
		} `json:"status"`
	} `json:"items"`
}

// CheckPDBObjects verifies at least one PodDisruptionBudget actually exists for the
// orchestration component in the given namespace/release, independent of what
// values.yaml declares — catching cases where the object was never applied, was
// deleted out-of-band, or targets the wrong label selector.
func CheckPDBObjects(namespace, releaseName string) []rules.Finding {
	var list pdbList
	// The chart's orchestration/Zeebe StatefulSet and its PDB carry the legacy
	// "zeebe-broker" compatibility component label, not "orchestration" — verified
	// against a real rendered release; do not "simplify" this back to "orchestration".
	sel := fmt.Sprintf("app.kubernetes.io/instance=%s,app.kubernetes.io/component=zeebe-broker", releaseName)
	if err := runJSON([]string{"get", "pdb", "-n", namespace, "-l", sel, "-o", "json"}, &list); err != nil {
		return []rules.Finding{{
			RuleID:   "CCD011",
			Severity: rules.Medium,
			Title:    "Could not verify PodDisruptionBudget objects live",
			Detail:   err.Error(),
		}}
	}
	if len(list.Items) == 0 {
		return []rules.Finding{{
			RuleID:   "CCD011",
			Severity: rules.High,
			Title:    "No PodDisruptionBudget object found for the orchestration component",
			Detail: fmt.Sprintf(
				"selector %q matched zero PodDisruptionBudget objects in namespace %q, regardless of "+
					"what values.yaml declares for this release.", sel, namespace),
			Remediation: "Confirm orchestration.podDisruptionBudget.enabled is true and re-run helm upgrade.",
		}}
	}
	var findings []rules.Finding
	for _, item := range list.Items {
		if item.Status.DisruptionsAllowed == 0 {
			findings = append(findings, rules.Finding{
				RuleID:   "CCD011",
				Severity: rules.Medium,
				Title:    fmt.Sprintf("PodDisruptionBudget %s currently allows zero disruptions", item.Metadata.Name),
				Detail: "This can be transient (a broker is currently down) or a permanent " +
					"misconfiguration (minAvailable set too high for the current replica count). " +
					"Investigate before assuming this PDB protects the cluster.",
			})
		}
	}
	return findings
}

type secretObj struct {
	Data map[string]string `json:"data"`
}

// CheckSecretKeyExists confirms a referenced Secret and key actually exist live —
// catches drift where values.yaml declares existingSecret/existingSecretKey but the
// object was never created, was renamed, or had the key removed after install.
func CheckSecretKeyExists(namespace, secretName, key, contextPath string) []rules.Finding {
	var s secretObj
	if err := runJSON([]string{"get", "secret", secretName, "-n", namespace, "-o", "json"}, &s); err != nil {
		return []rules.Finding{{
			RuleID:      "CCD012",
			Severity:    rules.High,
			Title:       fmt.Sprintf("Referenced Secret %q does not exist", secretName),
			Path:        contextPath,
			Detail:      fmt.Sprintf("values path %s references existingSecret=%q, not found in namespace %q: %v", contextPath, secretName, namespace, err),
			Remediation: "Create the Secret before installing/upgrading, or correct the existingSecret name.",
		}}
	}
	if _, ok := s.Data[key]; ok {
		return nil
	}
	return []rules.Finding{{
		RuleID:      "CCD012",
		Severity:    rules.High,
		Title:       fmt.Sprintf("Secret %q exists but has no key %q", secretName, key),
		Path:        contextPath,
		Detail:      fmt.Sprintf("values path %s references existingSecretKey=%q, not present in the Secret's data.", contextPath, key),
		Remediation: "Add the key to the Secret, or correct existingSecretKey.",
	}}
}

// GetHelmUserValues returns ONLY the values the operator supplied, without the chart's
// defaults merged in. The upgrade planner needs this rather than the "-a" view: a
// deprecated key that is merely sitting at its chart default is not something the
// operator configured, and reporting it would bury the real findings in noise.
func GetHelmUserValues(namespace, release string) ([]byte, error) {
	cmd := exec.Command("helm", "get", "values", "-n", namespace, release, "-o", "yaml")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm get values failed: %w: %s", err, stderr.String())
	}
	return out.Bytes(), nil
}

// ReleaseMetadata is the subset of `helm get metadata` this tool uses.
type ReleaseMetadata struct {
	Chart      string `json:"chart"`
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
	Namespace  string `json:"namespace"`
}

// GetReleaseMetadata reports the chart version an installed release is running, so the
// operator does not have to tell the tool where they are starting from.
func GetReleaseMetadata(namespace, release string) (*ReleaseMetadata, error) {
	cmd := exec.Command("helm", "get", "metadata", "-n", namespace, release, "-o", "json")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm get metadata failed: %w: %s", err, stderr.String())
	}
	var md ReleaseMetadata
	if err := json.Unmarshal(out.Bytes(), &md); err != nil {
		return nil, fmt.Errorf("parsing helm get metadata output: %w", err)
	}
	return &md, nil
}
