package health

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type infoResponse struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// NewInfoHandler returns an http.HandlerFunc that serves build metadata.
// All parameters are set at binary build time via ldflags; pass "unknown" as
// fallback when building locally without ldflags.
func NewInfoHandler(component, version, gitCommit, buildTime string) http.HandlerFunc {
	resp := infoResponse{
		Component: component,
		Version:   version,
		GitCommit: gitCommit,
		BuildTime: buildTime,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		// infoResponse contains only string fields — Marshal can only fail if a future
		// field addition introduces an un-serialisable type. Panic immediately so the
		// programming error is caught at startup rather than silently at request time.
		panic(fmt.Sprintf("info: json.Marshal failed: %v", err))
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
