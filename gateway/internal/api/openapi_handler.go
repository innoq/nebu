//go:build go1.22

package api

import (
	"net/http"

	apispec "github.com/nebu/nebu/api"
)

// OpenAPIYAMLHandler serves the raw openapi.yaml spec file.
// The endpoint is unauthenticated per FR51 — API tooling must be able to fetch the spec
// without credentials.
func OpenAPIYAMLHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(apispec.Spec)
}
