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

// MatchesAdminGroupClaim returns true if any string value across all OIDC claims
// equals adminGroupClaim. Handles both plain string claims and []interface{} array
// claims (e.g. Dex "groups", Keycloak "roles"). All elements of all array claims
// are checked — not just the first element.
func MatchesAdminGroupClaim(claims map[string]interface{}, adminGroupClaim string) bool {
	if adminGroupClaim == "" {
		return false
	}
	for _, v := range claims {
		switch val := v.(type) {
		case string:
			if val == adminGroupClaim {
				return true
			}
		case []interface{}:
			for _, item := range val {
				if s, ok := item.(string); ok && s == adminGroupClaim {
					return true
				}
			}
		}
	}
	return false
}
