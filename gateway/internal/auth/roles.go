package auth

// MapSystemRole converts a raw OIDC claim value to a canonical Nebu system role.
// Only "instance_admin" and "compliance_officer" are privileged roles.
// All other values (including empty string) map to "user".
func MapSystemRole(rawClaim string) string {
	switch rawClaim {
	case "instance_admin", "compliance_officer":
		return rawClaim
	default:
		return "user"
	}
}
