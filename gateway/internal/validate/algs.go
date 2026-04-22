package validate

import (
	"os"
	"strings"
)

// SupportedAlgs reads NEBU_OIDC_SUPPORTED_ALGS (comma-separated).
// Defaults to ["RS256"] when unset or empty.
func SupportedAlgs() []string {
	raw := strings.TrimSpace(os.Getenv("NEBU_OIDC_SUPPORTED_ALGS"))
	if raw == "" {
		return []string{"RS256"}
	}
	parts := strings.Split(raw, ",")
	algs := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			algs = append(algs, t)
		}
	}
	if len(algs) == 0 {
		return []string{"RS256"}
	}
	return algs
}
