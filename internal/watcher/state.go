// Package watcher implements the continuous-drift check: rerun the same live
// findings on a schedule, remember what was already reported, and only surface what
// is actually new — so a CronJob running every 30 minutes doesn't repeat itself.
package watcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
)

func kubectlBin() string {
	if _, err := exec.LookPath("kubectl"); err == nil {
		return "kubectl"
	}
	return "oc"
}

// Key identifies a finding for the purpose of "have we already reported this" —
// not full equality, since Detail text can legitimately vary run to run (e.g. an
// exact restart count) without the underlying problem being a new one.
func Key(f rules.Finding) string {
	return f.RuleID + "|" + f.Path
}

type configMap struct {
	Data map[string]string `json:"data"`
}

// LoadState reads the previous run's set of finding keys from a ConfigMap. ok is
// false when no state exists yet (first run ever) — callers should then treat every
// current finding as new, not fail.
func LoadState(namespace, configMapName string) (keys map[string]bool, ok bool, err error) {
	cmd := exec.Command(kubectlBin(), "get", "configmap", configMapName, "-n", namespace, "-o", "json")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, false, nil // treat "not found" (and any other get error) as "no prior state"
	}
	var cm configMap
	if err := json.Unmarshal(out.Bytes(), &cm); err != nil {
		return nil, false, fmt.Errorf("parsing existing state configmap: %w", err)
	}
	var list []string
	if raw, present := cm.Data["findingKeys"]; present {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, false, fmt.Errorf("parsing findingKeys in state configmap: %w", err)
		}
	}
	keys = make(map[string]bool, len(list))
	for _, k := range list {
		keys[k] = true
	}
	return keys, true, nil
}

// SaveState writes the current set of finding keys back to the ConfigMap, creating
// it on first run. Uses `kubectl apply` (declarative) so it works whether or not
// the ConfigMap already exists.
func SaveState(namespace, configMapName string, keys []string) error {
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	manifest := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  findingKeys: %q
`, configMapName, namespace, string(raw))

	cmd := exec.Command(kubectlBin(), "apply", "-f", "-")
	cmd.Stdin = bytes.NewBufferString(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w: %s", err, stderr.String())
	}
	return nil
}
