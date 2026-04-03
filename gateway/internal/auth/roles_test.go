package auth

import "testing"

func TestMapSystemRole_InstanceAdmin(t *testing.T) {
	if got := MapSystemRole("instance_admin"); got != "instance_admin" {
		t.Errorf("MapSystemRole(instance_admin) = %q, want instance_admin", got)
	}
}

func TestMapSystemRole_ComplianceOfficer(t *testing.T) {
	if got := MapSystemRole("compliance_officer"); got != "compliance_officer" {
		t.Errorf("MapSystemRole(compliance_officer) = %q, want compliance_officer", got)
	}
}

func TestMapSystemRole_UnknownValue(t *testing.T) {
	if got := MapSystemRole("superuser"); got != "user" {
		t.Errorf("MapSystemRole(superuser) = %q, want user", got)
	}
}

func TestMapSystemRole_EmptyString(t *testing.T) {
	if got := MapSystemRole(""); got != "user" {
		t.Errorf("MapSystemRole(\"\") = %q, want user", got)
	}
}

func TestExtractRoleClaim_StringClaim(t *testing.T) {
	claims := map[string]interface{}{"nebu_role": "instance_admin"}
	if got := ExtractRoleClaim(claims, "nebu_role"); got != "instance_admin" {
		t.Errorf("got %q, want instance_admin", got)
	}
}

func TestExtractRoleClaim_MissingClaim(t *testing.T) {
	claims := map[string]interface{}{}
	if got := ExtractRoleClaim(claims, "nebu_role"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractRoleClaim_ArraySingleAdmin(t *testing.T) {
	// Dex returns groups as []interface{} — single element
	claims := map[string]interface{}{"groups": []interface{}{"instance_admin"}}
	if got := ExtractRoleClaim(claims, "groups"); got != "instance_admin" {
		t.Errorf("got %q, want instance_admin", got)
	}
}

func TestExtractRoleClaim_ArrayAdminNotFirst(t *testing.T) {
	// THE BUG CASE: admin role is not the first element.
	// Previous extractRoleClaim returned "viewer" (→ "user"). Must return "instance_admin".
	claims := map[string]interface{}{"groups": []interface{}{"viewer", "instance_admin"}}
	if got := ExtractRoleClaim(claims, "groups"); got != "instance_admin" {
		t.Errorf("got %q, want instance_admin", got)
	}
}

func TestExtractRoleClaim_ArrayComplianceNotFirst(t *testing.T) {
	claims := map[string]interface{}{"groups": []interface{}{"viewer", "compliance_officer"}}
	if got := ExtractRoleClaim(claims, "groups"); got != "compliance_officer" {
		t.Errorf("got %q, want compliance_officer", got)
	}
}

func TestExtractRoleClaim_ArrayAdminBeatsCompliance(t *testing.T) {
	// instance_admin takes priority over compliance_officer
	claims := map[string]interface{}{"groups": []interface{}{"compliance_officer", "instance_admin"}}
	if got := ExtractRoleClaim(claims, "groups"); got != "instance_admin" {
		t.Errorf("got %q, want instance_admin", got)
	}
}

func TestExtractRoleClaim_ArrayNoPrivilegedRole(t *testing.T) {
	// No privileged role — returns first element
	claims := map[string]interface{}{"groups": []interface{}{"viewer", "editor"}}
	if got := ExtractRoleClaim(claims, "groups"); got != "viewer" {
		t.Errorf("got %q, want viewer (first element)", got)
	}
}

func TestExtractRoleClaim_EmptyArray(t *testing.T) {
	claims := map[string]interface{}{"groups": []interface{}{}}
	if got := ExtractRoleClaim(claims, "groups"); got != "" {
		t.Errorf("got %q, want empty string for empty array", got)
	}
}

// MatchesAdminGroupClaim tests — used by CallbackHandler to gate admin UI access.

func TestMatchesAdminGroupClaim_StringMatch(t *testing.T) {
	claims := map[string]interface{}{"nebu_role": "instance_admin"}
	if !MatchesAdminGroupClaim(claims, "instance_admin") {
		t.Error("expected match for string claim")
	}
}

func TestMatchesAdminGroupClaim_StringNoMatch(t *testing.T) {
	claims := map[string]interface{}{"nebu_role": "viewer"}
	if MatchesAdminGroupClaim(claims, "instance_admin") {
		t.Error("expected no match")
	}
}

func TestMatchesAdminGroupClaim_ArrayContainsClaim(t *testing.T) {
	claims := map[string]interface{}{"groups": []interface{}{"viewer", "instance_admin"}}
	if !MatchesAdminGroupClaim(claims, "instance_admin") {
		t.Error("expected match: instance_admin in array")
	}
}

func TestMatchesAdminGroupClaim_ArrayDoesNotContainClaim(t *testing.T) {
	claims := map[string]interface{}{"groups": []interface{}{"viewer", "editor"}}
	if MatchesAdminGroupClaim(claims, "instance_admin") {
		t.Error("expected no match: instance_admin not in array")
	}
}

func TestMatchesAdminGroupClaim_CustomClaim(t *testing.T) {
	// Enterprise deployment with a custom admin group name (e.g. "corp_admin").
	claims := map[string]interface{}{"groups": []interface{}{"corp_users", "corp_admin"}}
	if !MatchesAdminGroupClaim(claims, "corp_admin") {
		t.Error("expected match for custom admin claim")
	}
}

func TestMatchesAdminGroupClaim_EmptyAdminClaim(t *testing.T) {
	// Empty adminGroupClaim must never match (prevents accidental grant-all).
	claims := map[string]interface{}{"groups": []interface{}{"instance_admin"}}
	if MatchesAdminGroupClaim(claims, "") {
		t.Error("empty adminGroupClaim must return false")
	}
}

func TestMatchesAdminGroupClaim_NoClaims(t *testing.T) {
	claims := map[string]interface{}{}
	if MatchesAdminGroupClaim(claims, "instance_admin") {
		t.Error("expected no match for empty claims map")
	}
}
