//go:build go1.22

// Package api provides shared response types and helpers for the Admin API.
package api

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the standard Admin API response envelope.
// Data is always present in the JSON output (never omitted) — it is null on error responses.
// Meta and Error use omitempty so they are absent when nil.
type APIResponse[T any] struct {
	Data  T         `json:"data"`
	Meta  *Meta     `json:"meta,omitempty"`
	Error *APIError `json:"error,omitempty"`
}

// Meta carries pagination metadata for list endpoints.
// All fields use omitempty so zero-value fields are omitted from the JSON output.
type Meta struct {
	Total      int    `json:"total,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	PrevCursor string `json:"prev_cursor,omitempty"`
}

// APIError represents a structured API error included in the response envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse constructs an APIResponse[any] for error cases.
// Data is nil (serialises as JSON null), Error is populated.
func ErrorResponse(code, message string) APIResponse[any] {
	return APIResponse[any]{
		Data:  nil,
		Error: &APIError{Code: code, Message: message},
	}
}

// WriteJSON writes v as JSON to w with the given HTTP status code.
// Sets Content-Type: application/json.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
