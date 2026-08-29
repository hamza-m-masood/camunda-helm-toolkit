package bundle_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/bundle"
)

func TestRedactValues_RedactsCredentialShapedLeaves(t *testing.T) {
	in := map[string]interface{}{
		"orchestration": map[string]interface{}{
			"security": map[string]interface{}{
				"initialization": map[string]interface{}{
					"users": []interface{}{
						map[string]interface{}{
							"username": "demo",
							"password": "demo", // must be redacted (key "password")
							"email":    "demo@demo.com",
						},
					},
				},
			},
		},
		"global": map[string]interface{}{
			"elasticsearch": map[string]interface{}{
				"auth": map[string]interface{}{
					"secret": map[string]interface{}{
						"existingSecret":    "es-secret",   // under a "secret" key -> redacted
						"existingSecretKey": "password",    // also under "secret" -> redacted
						"inlineSecret":      "SuperS3cret", // definitely must be redacted
					},
				},
			},
		},
		"webModeler": map[string]interface{}{
			"restapi": map[string]interface{}{
				"externalDatabase": map[string]interface{}{
					"host": "postgres.internal", // NOT credential-shaped, must survive
				},
			},
		},
	}

	out := bundle.RedactValues(in)

	users := out["orchestration"].(map[string]interface{})["security"].(map[string]interface{})["initialization"].(map[string]interface{})["users"].([]interface{})
	u0 := users[0].(map[string]interface{})
	if u0["password"] != "<redacted>" {
		t.Errorf("password = %v, want <redacted>", u0["password"])
	}
	if u0["username"] != "demo" {
		t.Errorf("username was redacted but should not have been: %v", u0["username"])
	}
	if u0["email"] != "demo@demo.com" {
		t.Errorf("email was redacted but should not have been: %v", u0["email"])
	}

	secretBlock := out["global"].(map[string]interface{})["elasticsearch"].(map[string]interface{})["auth"].(map[string]interface{})["secret"].(map[string]interface{})
	for _, k := range []string{"existingSecret", "existingSecretKey", "inlineSecret"} {
		if secretBlock[k] != "<redacted>" {
			t.Errorf("global.elasticsearch.auth.secret.%s = %v, want <redacted>", k, secretBlock[k])
		}
	}

	host := out["webModeler"].(map[string]interface{})["restapi"].(map[string]interface{})["externalDatabase"].(map[string]interface{})["host"]
	if host != "postgres.internal" {
		t.Errorf("host was redacted but should not have been: %v", host)
	}
}

func TestRedactValues_MidSubstringKeyIsNotBlanketRedacted(t *testing.T) {
	in := map[string]interface{}{
		"keyboardShortcut": "ctrl-x", // contains "key" but not at the end -> not redacted
		"apiKey":           "abc123", // ends in "key" -> redacted
	}
	out := bundle.RedactValues(in)
	if out["keyboardShortcut"] != "ctrl-x" {
		t.Errorf("keyboardShortcut = %v, want unchanged", out["keyboardShortcut"])
	}
	if out["apiKey"] != "<redacted>" {
		t.Errorf("apiKey = %v, want <redacted>", out["apiKey"])
	}
}
