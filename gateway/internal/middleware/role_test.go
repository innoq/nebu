package middleware // NOT middleware_test — needs access to unexported mapRole

import "testing"

func TestMapRole_InstanceAdmin(t *testing.T) {
	if got := mapRole("instance_admin"); got != "instance_admin" {
		t.Errorf("mapRole(instance_admin) = %q, want instance_admin", got)
	}
}

func TestMapRole_ComplianceOfficer(t *testing.T) {
	if got := mapRole("compliance_officer"); got != "compliance_officer" {
		t.Errorf("mapRole(compliance_officer) = %q, want compliance_officer", got)
	}
}

func TestMapRole_UnknownValue(t *testing.T) {
	if got := mapRole("superuser"); got != "user" {
		t.Errorf("mapRole(superuser) = %q, want user", got)
	}
}

func TestMapRole_EmptyString(t *testing.T) {
	if got := mapRole(""); got != "user" {
		t.Errorf("mapRole(\"\") = %q, want user", got)
	}
}
