package middleware

import (
	"github.com/nebu/nebu/internal/validate"
)

// ParseSupportedAlgs reads NEBU_OIDC_SUPPORTED_ALGS (comma-separated).
// Defaults to ["RS256"] when unset or empty.
func ParseSupportedAlgs() []string {
	return validate.SupportedAlgs()
}
