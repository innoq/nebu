package validate

import (
	"fmt"
	"net/url"
)

// IssuerURL returns nil if s is a valid OIDC issuer URL for Nebu.
// Accepted: https://<any>, http://localhost[:<port>], http://127.0.0.1[:<port>], http://[::1][:<port>]
// Rejected: everything else.
func IssuerURL(s string) error {
	if s == "" {
		return fmt.Errorf("OIDC issuer URL must not be empty")
	}

	parsed, err := url.ParseRequestURI(s)
	if err != nil {
		return fmt.Errorf("OIDC issuer URL is not valid: %w", err)
	}

	if parsed.Scheme == "https" {
		return nil
	}

	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("OIDC issuer must use HTTPS (http://localhost allowed for dev)")
	}

	return fmt.Errorf("OIDC issuer must use HTTPS (http://localhost allowed for dev)")
}
