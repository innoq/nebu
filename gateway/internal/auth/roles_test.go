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
