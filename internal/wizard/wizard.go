// Package wizard builds a starter values.yaml overlay for `init` from a small, fixed
// set of answers -- not a general values.yaml editor, and deliberately not a schema-
// driven form over every field the chart exposes. The point of `init` is not "a valid
// values.yaml" (any hand-written one can be that); it is "a values.yaml that already
// passes every check rule this tool knows how to close via values.yaml alone" -- see
// Build's four documented exceptions for the ones it cannot close and why:
//
//   - CCD008 (digest pinning): the correct digest is specific to the exact chart
//     release installed, and this package has no way to know that in advance.
//   - CCD015 (grace period / heap dump path): a chart template limitation with no
//     values.yaml field at all -- no override can close it.
//   - CCD003 on identityKeycloak.* paths only: the 8.9 chart's own shipped default
//     sets identityKeycloak.auth.existingSecret / identityKeycloak.postgresql.auth.
//     existingSecret without existingSecretKey (confirmed: `check --chart` reports
//     the identical finding against the bare chart with no overlay at all -- this
//     predates and is unrelated to anything Answers controls). Fixing it would mean
//     guessing an existingSecretKey with no principled way to know it is correct,
//     unlike the CCD002 fix below, which only ever reads a value the same document
//     already ships. 8.10 removed the bundled subchart this path belongs to, so this
//     exception is inert there.
//   - CCD010 on the two ConfigMaps that render orchestration.security.initialization.
//     users verbatim: <fullname>-zeebe-configuration (templates/orchestration/files/
//     _application.yaml, `... | toYaml`) and <fullname>-connectors-configuration
//     (templates/connectors/files/_application.yaml, `$user := first
//     .Values.orchestration.security.initialization.users`) -- confirmed exhaustively
//     as the only two consumers of that field, on both 8.9 and 8.10. Whatever password
//     is set for the basic-auth admin user this package configures below lands in a
//     ConfigMap, not a Secret, with no values.yaml-level way to route it elsewhere --
//     the same category as CCD015, not something a different choice of value avoids.
//     Reproduced with a real (space-free) password via the compiled `init` binary
//     against the real chart before being added here, not assumed from reading the
//     template alone.
//
// See cmd/camunda-chart-doctor/init.go for the self-check that enforces every other
// rule stays clean.
package wizard

import (
	"fmt"
	"strings"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/capacityplan"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
	"github.com/hamza-m-masood/camunda-chart-doctor/internal/values"
)

// Answers is the fixed question set `init` asks.
type Answers struct {
	// ReleaseName is what the operator intends to `helm install <ReleaseName> ...` as.
	// It has to be known up front and baked in literally: unlike a chart template,
	// a raw values.yaml override is never itself run through Helm's {{ .Release.Name }}
	// templating, so the release-scoped anti-affinity fix (CCD016) has no other way
	// to know which release name to match against.
	ReleaseName string

	EnableIdentity   bool
	EnableConnectors bool
	EnableOptimize   bool
	EnableWebModeler bool
	// WebModelerFromEmail is required if EnableWebModeler is true: the chart's own
	// webModeler.restapi.mail.fromAddress is guarded by a hard Helm `required` template
	// call (templates/web-modeler/configmap-restapi.yaml) with no values.yaml default,
	// so `helm template`/`helm install` fails outright with Web Modeler enabled and this
	// unset -- confirmed by actually rendering the chart with it empty.
	WebModelerFromEmail string

	// SecondaryStorageType is elasticsearch|opensearch|rdbms. Required: the chart's
	// own constraints.tpl fails the render entirely without one set, on every version
	// this tool has been checked against (8.9, 8.10).
	SecondaryStorageType string

	// Sizing answers, passed straight through to internal/capacityplan -- this
	// package does not reimplement or duplicate that arithmetic. Leave ThroughputPerSec
	// at 0 to skip sizing and just apply the chart's own current orchestration
	// resource defaults as a Guaranteed-QoS fix instead (see Build).
	ThroughputPerSec float64
	AvgPayloadKB     float64
	RetentionDays    int

	IngressHost string
	IngressTLS  bool

	AuthMethod    string // basic | oidc
	AdminUsername string // required if AuthMethod == "basic"
	AdminPassword string // required if AuthMethod == "basic"
	OIDCIssuerURL string // required if AuthMethod == "oidc"
}

func (a Answers) validate() error {
	if a.ReleaseName == "" {
		return fmt.Errorf("ReleaseName is required")
	}
	switch a.SecondaryStorageType {
	case "elasticsearch", "opensearch", "rdbms":
	default:
		return fmt.Errorf("SecondaryStorageType must be elasticsearch, opensearch, or rdbms, got %q", a.SecondaryStorageType)
	}
	switch a.AuthMethod {
	case "basic":
		if a.AdminUsername == "" || a.AdminPassword == "" {
			return fmt.Errorf("AdminUsername and AdminPassword are required when AuthMethod is basic")
		}
	case "oidc":
		if a.OIDCIssuerURL == "" {
			return fmt.Errorf("OIDCIssuerURL is required when AuthMethod is oidc")
		}
	default:
		return fmt.Errorf("AuthMethod must be basic or oidc, got %q", a.AuthMethod)
	}
	if a.EnableWebModeler && a.WebModelerFromEmail == "" {
		return fmt.Errorf("WebModelerFromEmail is required when EnableWebModeler is true")
	}
	return nil
}

// Build produces a values.yaml overlay for a, plus follow-up notes to print alongside
// it (things it deliberately did not, or could not, fully resolve). chartDefaults is
// the target chart's own current values.yaml (e.g. from helmrender.ShowValues) -- used
// to read the chart's own currently-shipped resource limits so the Guaranteed-QoS fix
// below always matches whatever the chart ships today, rather than a number this
// package hardcodes at the time it was written.
func Build(a Answers, chartDefaults values.Values) (values.Values, []string, error) {
	if err := a.validate(); err != nil {
		return nil, nil, err
	}

	out := values.Values{}
	var notes []string

	values.SetPath(out, "orchestration.data.secondaryStorage.type", a.SecondaryStorageType)

	values.SetPath(out, "orchestration.podDisruptionBudget.enabled", true)
	values.SetPath(out, "orchestration.podDisruptionBudget.maxUnavailable", 1)

	values.SetPath(out, "orchestration.retention.enabled", true)
	values.SetPath(out, "orchestration.history.retention.enabled", true)

	values.SetPath(out, "orchestration.readinessProbe.timeoutSeconds", 5)

	// CCD016: the chart's own shipped default matches only app.kubernetes.io/component,
	// so a second release in the same namespace can never schedule and a drain has
	// nowhere else to place an evicted broker. Adding the instance term release-scopes it.
	values.SetPath(out, "orchestration.affinity.podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution",
		[]interface{}{
			map[string]interface{}{
				"labelSelector": map[string]interface{}{
					"matchExpressions": []interface{}{
						map[string]interface{}{
							"key": "app.kubernetes.io/component", "operator": "In",
							"values": []interface{}{"zeebe-broker"},
						},
						map[string]interface{}{
							"key": "app.kubernetes.io/instance", "operator": "In",
							"values": []interface{}{a.ReleaseName},
						},
					},
				},
				"topologyKey": "kubernetes.io/hostname",
			},
		})

	values.SetPath(out, "prometheusServiceMonitor.enabled", true)

	values.SetPath(out, "global.security.authentication.method", a.AuthMethod)

	// Independent of AuthMethod: orchestration.security.initialization.users is Zeebe's
	// own local user seed list, and the chart ships it defaulted to demo/demo regardless
	// of the global auth method. Leaving it untouched under OIDC would still pass CCD004
	// today only because that seed list happens to be inert while OIDC is active -- it
	// becomes a live demo/demo credential the moment anyone reverts to basic auth, so it
	// is always replaced here, not just when AuthMethod is basic.
	if a.AuthMethod == "basic" {
		values.SetPath(out, "orchestration.security.initialization.users", []interface{}{
			map[string]interface{}{
				"username": a.AdminUsername,
				"password": a.AdminPassword,
				"name":     "Administrator",
				"email":    "",
			},
		})
	} else {
		values.SetPath(out, "orchestration.security.initialization.users", []interface{}{})
	}

	if a.AuthMethod == "oidc" {
		values.SetPath(out, "global.identity.auth.issuerBackendUrl", a.OIDCIssuerURL)
		notes = append(notes,
			"OIDC: issuerBackendUrl is set, but each enabled component's own clientId/audience/"+
				"secret under global.identity.auth.<component> still needs to be configured to "+
				"match your external provider -- the chart's shipped defaults for those fields "+
				"only work with its own bundled/external Keycloak, not a generic external OIDC "+
				"provider. See the chart's 'Identity / Authentication Modes' documentation.")
	}

	for name, on := range map[string]bool{
		"identity": a.EnableIdentity, "connectors": a.EnableConnectors,
		"optimize": a.EnableOptimize, "webModeler": a.EnableWebModeler,
	} {
		values.SetPath(out, name+".enabled", on)
	}
	if a.EnableWebModeler {
		values.SetPath(out, "webModeler.restapi.mail.fromAddress", a.WebModelerFromEmail)
	}

	if a.IngressHost != "" {
		values.SetPath(out, "global.ingress.enabled", true)
		// global.host, not global.ingress.host: the latter was removed outright in 8.10
		// (templates/common/constraints.tpl's keyRemoved guard), and every template that
		// reads it already checks global.host first and falls back to global.ingress.host
		// on 8.9 -- so global.host is the version-stable field on both, confirmed by
		// reading templates/common/_helpers.tpl, not assumed from the values.yaml
		// description alone.
		values.SetPath(out, "global.host", a.IngressHost)
		values.SetPath(out, "global.ingress.tls.enabled", a.IngressTLS)
	}

	if a.ThroughputPerSec > 0 && a.AvgPayloadKB > 0 {
		rec := capacityplan.Recommend(capacityplan.Input{
			ThroughputPerSec: a.ThroughputPerSec,
			AvgPayloadKB:     a.AvgPayloadKB,
			RetentionDays:    a.RetentionDays,
		})
		values.SetPath(out, "orchestration.replicationFactor", fmt.Sprintf("%d", rec.ReplicationFactor))
		values.SetPath(out, "orchestration.clusterSize", fmt.Sprintf("%d", rec.ClusterSize))
		values.SetPath(out, "orchestration.partitionCount", fmt.Sprintf("%d", rec.PartitionCount))
		values.SetPath(out, "orchestration.pvcSize", fmt.Sprintf("%dGi", rec.PVCSizeGiB))
		values.SetPath(out, "orchestration.resources.requests.memory", fmt.Sprintf("%dMi", rec.MemoryLimitMiB))
		values.SetPath(out, "orchestration.resources.limits.memory", fmt.Sprintf("%dMi", rec.MemoryLimitMiB))
		notes = append(notes, rec.Reasoning...)
	}

	// CCD002 scans the ENTIRE effective values tree for any resources block with
	// requests.memory != limits.memory, regardless of whether the owning component is
	// enabled -- so closing it component-by-component above is not enough; every
	// mismatched block the chart ships by default (including ones this wizard never
	// otherwise touches, like bundled subchart defaults) has to be fixed too, or the
	// self-check in cmd/camunda-chart-doctor/init.go can never come back clean.
	// Genuinely no values.yaml has an "am I enabled" flag readable from a resources
	// block itself, so this is a blanket fix by design, not an oversight -- an entry
	// for a component the operator left disabled is a no-op today and a safety net if
	// they enable it later without revisiting this file.
	merged := values.DeepMerge(chartDefaults, out)
	for _, b := range values.FindBlocks(merged, "requests", "limits") {
		req, _ := b.Node["requests"].(map[string]interface{})
		lim, _ := b.Node["limits"].(map[string]interface{})
		if req == nil || lim == nil {
			continue
		}
		limMem, ok := lim["memory"]
		if !ok {
			continue
		}
		values.SetPath(out, b.Path+".requests.memory", limMem)
	}

	notes = append(notes,
		"Digest pinning (CCD008): this starter config intentionally does not pin image "+
			"digests -- the correct digest is specific to the exact chart release you install, "+
			"and hardcoding one here would go stale the moment that release changes. See the "+
			"chart's own values-digest.yaml for the digests shipped with your chosen version.",
		"Run `camunda-chart-doctor plan-secrets` against your first real install before your "+
			"next `helm upgrade`, to pin any Helm-auto-generated secret before it regenerates.",
	)
	if a.AuthMethod == "basic" {
		notes = append(notes,
			"Admin credential in a ConfigMap (CCD010): the chart renders this admin user's "+
				"password verbatim into <release>-zeebe-configuration (and, if Connectors is "+
				"enabled, <release>-connectors-configuration too) -- a ConfigMap, not a Secret, "+
				"regardless of the value. This is a known chart limitation, not something this "+
				"tool can fix via values.yaml; consider RBAC restricting ConfigMap read access "+
				"in this namespace if that matters in your environment.")
	}

	return out, notes, nil
}

// IsDocumentedException reports whether f is one of the three findings documented in
// this package's doc comment as unclosable via values.yaml -- the single definition
// both this package's own self-check tests and `init`'s runtime self-check share, so
// they cannot silently drift apart on what counts as an accepted exception.
func IsDocumentedException(f rules.Finding) bool {
	if f.RuleID == "CCD008" {
		return true
	}
	if f.RuleID == "CCD015" {
		return true
	}
	if f.RuleID == "CCD003" && strings.HasPrefix(f.Path, "identityKeycloak.") {
		return true
	}
	if f.RuleID == "CCD010" && (strings.HasSuffix(f.Path, "-zeebe-configuration") || strings.HasSuffix(f.Path, "-connectors-configuration")) {
		return true
	}
	return false
}
