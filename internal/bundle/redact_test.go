package bundle_test

import (
	"strings"
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

func TestRedactValues_RedactsEnvVarShapedNameValuePairs(t *testing.T) {
	in := map[string]interface{}{
		"orchestration": map[string]interface{}{
			"env": []interface{}{
				map[string]interface{}{"name": "MY_CUSTOM_PASSWORD", "value": "a-real-secret"},
				map[string]interface{}{"name": "WeIrD_CaSiNg_ToKeN", "value": "another-secret"},
				map[string]interface{}{"name": "JAVA_OPTS", "value": "-Xmx1024m"}, // must survive
			},
		},
	}
	out := bundle.RedactValues(in)
	env := out["orchestration"].(map[string]interface{})["env"].([]interface{})

	if v := env[0].(map[string]interface{})["value"]; v != "<redacted>" {
		t.Errorf("MY_CUSTOM_PASSWORD value = %v, want <redacted>", v)
	}
	if n := env[0].(map[string]interface{})["name"]; n != "MY_CUSTOM_PASSWORD" {
		t.Errorf("name was redacted but should not have been: %v", n)
	}
	if v := env[1].(map[string]interface{})["value"]; v != "<redacted>" {
		t.Errorf("WeIrD_CaSiNg_ToKeN value = %v, want <redacted> (case-insensitive match)", v)
	}
	if v := env[2].(map[string]interface{})["value"]; v != "-Xmx1024m" {
		t.Errorf("JAVA_OPTS value was redacted but should not have been: %v", v)
	}
}

func TestRedactDescribeText_RedactsCredentialShapedEnvLines(t *testing.T) {
	in := "    Environment:\n" +
		"      MY_CUSTOM_PASSWORD:  a-real-secret\n" +
		"      JAVA_OPTS:           -Xmx1024m\n" +
		"    Restart Count:  0\n"
	out := bundle.RedactDescribeText(in)

	if strings.Contains(out, "a-real-secret") {
		t.Errorf("credential value leaked into redacted describe text:\n%s", out)
	}
	if !strings.Contains(out, "MY_CUSTOM_PASSWORD:  <redacted>") {
		t.Errorf("expected the password line to read <redacted>, got:\n%s", out)
	}
	if !strings.Contains(out, "-Xmx1024m") {
		t.Errorf("non-credential env value was redacted but should not have been:\n%s", out)
	}
	if !strings.Contains(out, "Restart Count:  0") {
		t.Errorf("a multi-word describe heading was altered but should not have been:\n%s", out)
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
