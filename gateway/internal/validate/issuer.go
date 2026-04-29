package validate

import (
	"fmt"
	"net/url"
	"os"
)

// IssuerURL returns nil if s is a valid OIDC issuer URL for Nebu.
// Accepted: https://<any>, http://localhost[:<port>], http://127.0.0.1[:<port>], http://[::1][:<port>]
// Rejected: everything else.
//
// Dev-only escape hatch: if NEBU_ALLOW_INSECURE_OIDC_ISSUER=true AND NEBU_ENV
// is one of {dev,development,test,staging}, any http:// URL is accepted. This
// is required when the gateway reaches Dex via a Docker network hostname
// (e.g. http://dex:5556/dex).
//
// Fail-closed by default: an unset NEBU_ENV is treated as production and the
// override is refused. The operator must opt in explicitly to *both* the env
// label *and* the override flag — neither alone is enough.
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
		if os.Getenv("NEBU_ALLOW_INSECURE_OIDC_ISSUER") == "true" {
			// Fail-closed: only accept the override in an explicitly non-production
			// environment. An unset NEBU_ENV is treated as production.
			env := os.Getenv("NEBU_ENV")
			if env == "dev" || env == "development" || env == "test" || env == "staging" {
				return nil
			}
			return fmt.Errorf("NEBU_ALLOW_INSECURE_OIDC_ISSUER=true requires NEBU_ENV=dev|development|test|staging (got %q) — http:// issuers are forbidden in production", env)
		}
		return fmt.Errorf("OIDC issuer must use HTTPS (http://localhost allowed for dev; set NEBU_ALLOW_INSECURE_OIDC_ISSUER=true to allow other http:// hosts in dev)")
	}

	return fmt.Errorf("OIDC issuer must use HTTPS (http://localhost allowed for dev; set NEBU_ALLOW_INSECURE_OIDC_ISSUER=true to allow other http:// hosts in dev)")
}
