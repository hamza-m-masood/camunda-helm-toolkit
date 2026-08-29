// Package secretplan implements the secret-survival planner: for a live release, find
// every Secret the chart is auto-generating fresh on each render (randAlphaNum-style
// templates, not yet pinned via existingSecret) and produce the exact values overlay
// that freezes it to its current value before the next `helm upgrade` regenerates it.
package secretplan

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/live"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/values"
)

// mapping describes one chart-generated Secret this tool has verified against the
// real chart templates: which data key maps to which values.yaml "secret" block
// (the parent of existingSecret/existingSecretKey), so a precise, safe overlay can be
// generated rather than a generic warning.
type mapping struct {
	label           string
	nameSuffix      string
	keyToSecretPath map[string]string // data key -> dotted path to the secret{} block
}

// knownMappings is intentionally small and hand-verified, not a heuristic guess —
// each entry was confirmed by rendering the real chart and reading its secret
// template. Adding an entry here means it was checked the same way.
var knownMappings = []mapping{
	{
		label:      "Web Modeler Pusher secret/key",
		nameSuffix: "-web-modeler",
		keyToSecretPath: map[string]string{
			"pusher-app-secret": "webModeler.restapi.pusher.secret",
			"pusher-app-key":    "webModeler.restapi.pusher.client.secret",
		},
	},
}

// Confidence distinguishes a hand-verified mapping (exact overlay provided) from a
// generic finding for a Secret this tool doesn't recognize (advisory only).
type Confidence string

const (
	Known    Confidence = "known"
	Unmapped Confidence = "unmapped"
)

// Recommendation is one Secret this tool believes is worth pinning before the next
// upgrade, along with everything needed to act on it.
type Recommendation struct {
	SecretName     string
	Label          string
	Confidence     Confidence
	OverlayYAML    string   // populated only for Confidence == Known
	BackupCommands []string // read-only snapshot, for the record
	CreateCommands []string // populated only for Confidence == Known — creates the
	// new, chart-independent object the overlay's existingSecret actually points at
	Note string
}

func matchKnown(s live.SecretSummary) (mapping, bool) {
	for _, m := range knownMappings {
		if !strings.HasSuffix(s.Name, m.nameSuffix) {
			continue
		}
		hasAll := true
		for k := range m.keyToSecretPath {
			if !containsKey(s.Keys, k) {
				hasAll = false
				break
			}
		}
		if hasAll {
			return m, true
		}
	}
	return mapping{}, false
}

func containsKey(keys []string, k string) bool {
	for _, key := range keys {
		if key == k {
			return true
		}
	}
	return false
}

func alreadyPinned(effective values.Values, m mapping) bool {
	for _, secretPath := range m.keyToSecretPath {
		val, ok := values.GetPath(effective, secretPath+".existingSecret")
		es, _ := val.(string)
		if !ok || strings.TrimSpace(es) == "" {
			return false
		}
	}
	return true
}

func referencedByName(effective values.Values, name string) bool {
	for _, b := range values.FindBlocks(effective, "existingSecret") {
		if es, _ := b.Node["existingSecret"].(string); es == name {
			return true
		}
	}
	return false
}

// Plan cross-references live Secrets owned by a release against the effective values
// already in use, and returns one Recommendation per Secret that looks like it is
// still being auto-generated rather than pinned.
func Plan(effective values.Values, secrets []live.SecretSummary, namespace string) []Recommendation {
	var recs []Recommendation
	for _, s := range secrets {
		if m, ok := matchKnown(s); ok {
			if alreadyPinned(effective, m) {
				continue
			}
			recs = append(recs, buildKnownRecommendation(s, m, namespace))
			continue
		}
		if referencedByName(effective, s.Name) {
			continue
		}
		recs = append(recs, Recommendation{
			SecretName: s.Name,
			Label:      "unmapped chart-managed secret",
			Confidence: Unmapped,
			BackupCommands: []string{
				fmt.Sprintf("kubectl get secret %s -n %s -o yaml > %s-backup.yaml", s.Name, namespace, s.Name),
			},
			Note: "This Secret is owned by the release, but this tool has no verified " +
				"values.yaml mapping for it yet — it may or may not be regenerated on the " +
				"next helm upgrade. Back it up, and check the chart's README for an " +
				"existingSecret field on the component that owns it.",
		})
	}
	return recs
}

// buildKnownRecommendation is deliberately NOT "point existingSecret at the Secret's
// own current name". That was tried and disproven end-to-end on a real cluster: once
// every field the chart's own hasSecretConfig check looks at is satisfied by
// existingSecret, the chart's template stops rendering that Secret object at all —
// and helm upgrade then PRUNES it as no longer part of the release, deleting the very
// value this is trying to preserve. The only safe pattern is to copy the current
// value to a NEW, independently-named object the chart never owns, and point
// existingSecret at that new name instead.
func buildKnownRecommendation(s live.SecretSummary, m mapping, namespace string) Recommendation {
	pinnedName := s.Name + "-pinned"

	overlayYAML, err := renderOverlay(m, pinnedName)
	if err != nil {
		// Should be unreachable — every knownMappings entry's paths are static,
		// hand-written strings — but never silently ship a broken/empty overlay.
		overlayYAML = fmt.Sprintf("# error building overlay: %v\n", err)
	}
	overlay := fmt.Sprintf(
		"# References %s, a copy of the current live values that the chart does not\n"+
			"# own (see the create-command above) — NOT the original %s, which would stop\n"+
			"# being rendered once every field below is set and would be pruned by the\n"+
			"# next helm upgrade.\n%s", pinnedName, s.Name, overlayYAML)

	return Recommendation{
		SecretName:  s.Name,
		Label:       m.label,
		Confidence:  Known,
		OverlayYAML: overlay,
		BackupCommands: []string{
			fmt.Sprintf("kubectl get secret %s -n %s -o yaml > %s-backup.yaml", s.Name, namespace, s.Name),
		},
		CreateCommands: []string{
			// Labels are stripped, not just ownerReferences: this Secret's whole
			// point is to no longer be selectable as "owned by this release" —
			// verified end-to-end that skipping this makes the copy itself show up
			// as a false-positive "unmapped chart-managed secret" on the next run.
			fmt.Sprintf(
				"kubectl get secret %s -n %s -o json | "+
					"jq 'del(.metadata.ownerReferences, .metadata.resourceVersion, .metadata.uid, .metadata.creationTimestamp, .metadata.labels, .metadata.annotations) | .metadata.name=\"%s\"' | "+
					"kubectl apply -f -",
				s.Name, namespace, pinnedName),
		},
		Note: "1) Run the create-command below to copy the current secret to an " +
			"independent object the chart will never own or prune. 2) Apply the overlay " +
			"below with the next helm upgrade (merge it with your existing values, don't " +
			"replace them). 3) Confirm with `camunda-helm-toolkit check --release ... " +
			"--live` that CCD003/CCD012 are clean.",
	}
}

// renderOverlay builds a single, correctly-nested YAML document covering every
// secret-block path this mapping touches (e.g. "webModeler.restapi.pusher.secret" and
// "webModeler.restapi.pusher.client.secret" merge into one webModeler: tree rather
// than rendering as two separate flattened top-level keys, which would not be valid
// Helm values.yaml), pointing existingSecret at the Secret's own current name and
// existingSecretKey at the data key that already lives there.
func renderOverlay(m mapping, secretName string) (string, error) {
	root := map[string]interface{}{}
	for key, path := range m.keyToSecretPath {
		setPath(root, strings.Split(path, "."), map[string]interface{}{
			"existingSecret":    secretName,
			"existingSecretKey": key,
		})
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// setPath deep-merges leaf into root at the given dotted-path segments, creating
// intermediate maps as needed.
func setPath(root map[string]interface{}, segments []string, leaf map[string]interface{}) {
	cur := root
	for _, seg := range segments[:len(segments)-1] {
		next, ok := cur[seg].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segments[len(segments)-1]] = leaf
}
