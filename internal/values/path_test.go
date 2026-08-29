package values

import "testing"

func TestSetPathCreatesIntermediateMaps(t *testing.T) {
	v := Values{}
	SetPath(v, "identityKeycloak.auth.adminUser", "admin")
	got, ok := GetPath(v, "identityKeycloak.auth.adminUser")
	if !ok || got != "admin" {
		t.Fatalf("GetPath after SetPath = %v, %v; want admin, true", got, ok)
	}
}

func TestDeletePathPrunesEmptyParents(t *testing.T) {
	v := Values{"global": map[string]interface{}{
		"license": map[string]interface{}{"key": "abc"},
	}}
	if !DeletePath(v, "global.license.key") {
		t.Fatal("DeletePath reported no deletion")
	}
	if _, ok := GetPath(v, "global"); ok {
		t.Fatalf("empty parents were not pruned: %#v", v)
	}
}

func TestDeletePathKeepsSiblings(t *testing.T) {
	v := Values{"global": map[string]interface{}{
		"license":  map[string]interface{}{"key": "abc"},
		"identity": map[string]interface{}{"auth": "keycloak"},
	}}
	DeletePath(v, "global.license.key")
	if _, ok := GetPath(v, "global.identity.auth"); !ok {
		t.Fatalf("sibling was pruned: %#v", v)
	}
}

func TestDeletePathMissingKey(t *testing.T) {
	v := Values{"a": map[string]interface{}{"b": 1}}
	if DeletePath(v, "a.nope") {
		t.Fatal("DeletePath reported a deletion for a missing key")
	}
	if DeletePath(v, "x.y.z") {
		t.Fatal("DeletePath reported a deletion for a missing branch")
	}
}

func TestDeepCopyIsIndependent(t *testing.T) {
	orig := Values{"a": map[string]interface{}{"b": []interface{}{1, 2}}}
	cp := DeepCopy(orig)
	SetPath(cp, "a.b", "replaced")
	if got, _ := GetPath(orig, "a.b"); got == "replaced" {
		t.Fatal("DeepCopy shares state with the original")
	}
}

func TestMoveSubtree(t *testing.T) {
	v := Values{"camundaHub": map[string]interface{}{
		"webModeler": map[string]interface{}{
			"restapi": map[string]interface{}{"image": map[string]interface{}{"tag": "8.9"}},
			"enabled": true,
		},
	}}
	moved, conflicts := MoveSubtree(v, "camundaHub.webModeler", "camundaHub")
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}
	if len(moved) != 2 {
		t.Fatalf("moved = %v, want 2 leaves", moved)
	}
	if got, ok := GetPath(v, "camundaHub.restapi.image.tag"); !ok || got != "8.9" {
		t.Fatalf("leaf not moved: %#v", v)
	}
	if _, ok := GetPath(v, "camundaHub.webModeler"); ok {
		t.Fatalf("source prefix not removed: %#v", v)
	}
}

func TestMoveSubtreeReportsConflictsWithoutOverwriting(t *testing.T) {
	v := Values{"camundaHub": map[string]interface{}{
		"enabled":    "existing",
		"webModeler": map[string]interface{}{"enabled": "incoming"},
	}}
	_, conflicts := MoveSubtree(v, "camundaHub.webModeler", "camundaHub")
	if len(conflicts) != 1 || conflicts[0] != "camundaHub.enabled" {
		t.Fatalf("conflicts = %v, want [camundaHub.enabled]", conflicts)
	}
	got, _ := GetPath(v, "camundaHub.enabled")
	if got != "existing" {
		t.Fatalf("destination was overwritten: got %v, want existing", got)
	}
}

func TestMoveSubtreeMissingSource(t *testing.T) {
	v := Values{"a": map[string]interface{}{"b": 1}}
	moved, conflicts := MoveSubtree(v, "nope.here", "elsewhere")
	if moved != nil || conflicts != nil {
		t.Fatalf("moved=%v conflicts=%v, want nil, nil", moved, conflicts)
	}
}
