package live

import "fmt"

// SecretSummary is what plan-secrets needs to know about a live Secret: identity and
// key names only — never values. Nothing in this package or its callers should ever
// read or print .data's actual contents.
type SecretSummary struct {
	Name string
	Type string
	Keys []string
}

type secretList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Type string            `json:"type"`
		Data map[string]string `json:"data"`
	} `json:"items"`
}

// nonConfigSecretTypes are Secret types Kubernetes/Helm itself manages for a purpose
// unrelated to application configuration — never candidates for a "should this be
// pinned via existingSecret" recommendation.
var nonConfigSecretTypes = map[string]bool{
	"kubernetes.io/dockercfg":             true,
	"kubernetes.io/dockerconfigjson":      true,
	"kubernetes.io/service-account-token": true,
	"helm.sh/release.v1":                  true,
}

// ListOwnedSecrets returns every Secret in namespace labeled as owned by release,
// excluding Kubernetes/Helm-internal secret types (image pull secrets, service
// account tokens, Helm's own release-storage secrets) that are never candidates for
// an existingSecret pinning recommendation.
func ListOwnedSecrets(namespace, release string) ([]SecretSummary, error) {
	var list secretList
	sel := fmt.Sprintf("app.kubernetes.io/instance=%s", release)
	if err := runJSON([]string{"get", "secret", "-n", namespace, "-l", sel, "-o", "json"}, &list); err != nil {
		return nil, err
	}
	var out []SecretSummary
	for _, item := range list.Items {
		if nonConfigSecretTypes[item.Type] {
			continue
		}
		s := SecretSummary{Name: item.Metadata.Name, Type: item.Type}
		for k := range item.Data {
			s.Keys = append(s.Keys, k)
		}
		out = append(out, s)
	}
	return out, nil
}
