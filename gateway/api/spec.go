// Package apispec exposes the embedded openapi.yaml spec for use by handlers.
package apispec

import _ "embed"

// Spec holds the raw bytes of gateway/api/openapi.yaml, embedded at compile time.
//
//go:embed openapi.yaml
var Spec []byte
