package admin

import (
	"github.com/nebu/nebu/internal/validate"
)

// validateIssuerURL returns nil if s is a valid OIDC issuer URL.
// Accepted: https://<any>, http://localhost[:<port>], http://127.0.0.1[:<port>], http://[::1][:<port>]
// Rejected: empty, no scheme, http://<non-local>
//
// This is a package-level wrapper around validate.IssuerURL so that existing
// call sites inside the admin package do not need to be updated.
func validateIssuerURL(s string) error {
	return validate.IssuerURL(s)
}
