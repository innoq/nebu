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

// ExtractRoleClaim reads a named claim from JWT claims and returns the most privileged
// role string found. Handles both plain string claims and []interface{} array claims
// (e.g. Dex "groups", Keycloak "roles").
//
// Priority for array claims: "instance_admin" > "compliance_officer" > first element > "".
// This ensures that a user with claims ["viewer", "instance_admin"] receives "instance_admin",
// not "viewer" (which would be demoted to "user" by MapSystemRole).
func ExtractRoleClaim(claims map[string]interface{}, claimName string) string {
	v, ok := claims[claimName]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	arr, ok := v.([]interface{})
	if !ok || len(arr) == 0 {
		return ""
	}
	first := ""
	hasCompliance := false
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		if first == "" {
			first = s
		}
		if s == "instance_admin" {
			return s // highest privilege — return immediately
		}
		if s == "compliance_officer" {
			hasCompliance = true
		}
	}
	if hasCompliance {
		return "compliance_officer"
	}
	return first
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
