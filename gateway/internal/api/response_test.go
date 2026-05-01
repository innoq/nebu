//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.2:
// Admin API Response Format + Cursor-Pagination.
//
// RED PHASE — all tests fail until implementation is complete.
// Remove the t.Skip(...) call in each test to activate it during implementation.
//
// Covered Acceptance Criteria:
//   - AC#1  APIResponse[T], Meta, APIError types defined with exact JSON tags
//   - AC#4  Error responses: data is null (not omitted), error is populated
//   - AC#5  Unit tests: error response JSON has "data":null, meta absent on error,
//           error absent on success, meta included when non-nil
package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/api"
)

// TestAPIResponse_ErrorResponse_DataIsNull covers AC#4 + AC#5:
// When constructing an error response, the marshalled JSON must contain "data":null
// (explicitly present, not omitted) and "error" must be populated.
//
// [P0] — "data":null is an architectural invariant of the Admin API envelope;
// omitting the key would break API consumers that rely on the fixed schema.
func TestAPIResponse_ErrorResponse_DataIsNull(t *testing.T) {

	resp := api.APIResponse[any]{
		Data:  nil,
		Error: &api.APIError{Code: "M_NOT_FOUND", Message: "not found"},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#4] json.Marshal failed: %v", err)
	}
	body := string(raw)

	// "data":null must be present (not omitted).
	if !strings.Contains(body, `"data":null`) {
		t.Errorf(`[AC#4] expected JSON to contain "data":null; got: %s`, body)
	}

	// "error" must contain the code and message.
	if !strings.Contains(body, `"code":"M_NOT_FOUND"`) {
		t.Errorf(`[AC#4] expected JSON to contain "code":"M_NOT_FOUND"; got: %s`, body)
	}
	if !strings.Contains(body, `"message":"not found"`) {
		t.Errorf(`[AC#4] expected JSON to contain "message":"not found"; got: %s`, body)
	}
}

// TestAPIResponse_ErrorResponse_MetaAbsent covers AC#1 (omitempty on Meta) + AC#5:
// An error response with nil Meta must NOT include the "meta" key in the JSON output.
//
// [P1] — spurious "meta":null on error responses violates the response envelope contract.
func TestAPIResponse_ErrorResponse_MetaAbsent(t *testing.T) {

	resp := api.APIResponse[any]{
		Data:  nil,
		Meta:  nil,
		Error: &api.APIError{Code: "M_FORBIDDEN", Message: "forbidden"},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#1] json.Marshal failed: %v", err)
	}
	body := string(raw)

	if strings.Contains(body, `"meta"`) {
		t.Errorf(`[AC#1] expected "meta" key to be absent on error response (omitempty); got: %s`, body)
	}
}

// TestAPIResponse_SuccessResponse_ErrorAbsent covers AC#1 (omitempty on Error) + AC#5:
// A success response with nil Error must NOT include the "error" key in JSON output.
//
// [P0] — including "error":null on success responses would confuse API consumers.
func TestAPIResponse_SuccessResponse_ErrorAbsent(t *testing.T) {

	type UserPayload struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	resp := api.APIResponse[UserPayload]{
		Data:  UserPayload{ID: "u-123", Name: "Alice"},
		Meta:  nil,
		Error: nil,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#1] json.Marshal failed: %v", err)
	}
	body := string(raw)

	if strings.Contains(body, `"error"`) {
		t.Errorf(`[AC#1] expected "error" key to be absent on success response (omitempty); got: %s`, body)
	}

	// data must be present and populated.
	if !strings.Contains(body, `"data"`) {
		t.Errorf(`[AC#1] expected "data" key in success response; got: %s`, body)
	}
}

// TestAPIResponse_MetaIncludedWhenNonNil covers AC#1 + AC#5:
// When Meta is non-nil, the "meta" key with correct fields must appear in the JSON output.
//
// [P1] — the Meta struct carries pagination cursors; incorrect serialization breaks clients.
func TestAPIResponse_MetaIncludedWhenNonNil(t *testing.T) {

	type ListPayload struct {
		Items []string `json:"items"`
	}

	resp := api.APIResponse[ListPayload]{
		Data: ListPayload{Items: []string{"room-1", "room-2"}},
		Meta: &api.Meta{
			Total:      2,
			NextCursor: "eyJhZnRlcl9pZCI6InJvb20tMiIsImFmdGVyX2NyZWF0ZWRfYXQiOiIyMDI2LTAxLTE1VDEwOjMwOjAwWiJ9",
		},
		Error: nil,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#1] json.Marshal failed: %v", err)
	}
	body := string(raw)

	if !strings.Contains(body, `"meta"`) {
		t.Errorf(`[AC#1] expected "meta" key in response; got: %s`, body)
	}
	if !strings.Contains(body, `"total":2`) {
		t.Errorf(`[AC#1] expected "total":2 in meta; got: %s`, body)
	}
	if !strings.Contains(body, `"next_cursor"`) {
		t.Errorf(`[AC#1] expected "next_cursor" in meta; got: %s`, body)
	}
}

// TestAPIResponse_MetaOmitsZeroFields covers AC#1 (omitempty on Meta fields) + AC#5:
// Meta fields with zero-values (Total=0, empty cursors) must be omitted from JSON.
//
// [P2] — sending total:0 or empty cursor strings is noisy; omitempty keeps the envelope clean.
func TestAPIResponse_MetaOmitsZeroFields(t *testing.T) {

	resp := api.APIResponse[[]string]{
		Data:  []string{},
		Meta:  &api.Meta{Total: 0, NextCursor: "", PrevCursor: ""},
		Error: nil,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#1] json.Marshal failed: %v", err)
	}
	body := string(raw)

	// All zero-value fields in Meta must be omitted (omitempty).
	if strings.Contains(body, `"total"`) {
		t.Errorf(`[AC#1] expected "total" to be omitted when zero (omitempty); got: %s`, body)
	}
	if strings.Contains(body, `"next_cursor"`) {
		t.Errorf(`[AC#1] expected "next_cursor" to be omitted when empty (omitempty); got: %s`, body)
	}
	if strings.Contains(body, `"prev_cursor"`) {
		t.Errorf(`[AC#1] expected "prev_cursor" to be omitted when empty (omitempty); got: %s`, body)
	}
}

// TestAPIResponse_StructFieldNames covers AC#1 (exact field names and JSON tags):
// Marshalled JSON must use the exact field names specified in the acceptance criteria.
//
// [P0] — wrong JSON keys break the API contract for all clients.
func TestAPIResponse_StructFieldNames(t *testing.T) {

	resp := api.APIResponse[any]{
		Data: nil,
		Error: &api.APIError{
			Code:    "M_UNKNOWN",
			Message: "something went wrong",
		},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("[AC#1] json.Marshal failed: %v", err)
	}

	// Unmarshal into a plain map to verify exact key names.
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("[AC#1] json.Unmarshal failed: %v", err)
	}

	// Top-level keys must be "data" and "error" (not "Data", "Error", etc.).
	if _, ok := decoded["data"]; !ok {
		t.Errorf("[AC#1] expected top-level key %q in JSON; available keys: %v", "data", mapKeys(decoded))
	}
	if _, ok := decoded["error"]; !ok {
		t.Errorf("[AC#1] expected top-level key %q in JSON; available keys: %v", "error", mapKeys(decoded))
	}

	// Verify the error object has "code" and "message" keys (not "Code", "Message").
	var errObj map[string]json.RawMessage
	if err := json.Unmarshal(decoded["error"], &errObj); err != nil {
		t.Fatalf("[AC#1] failed to unmarshal error object: %v", err)
	}
	if _, ok := errObj["code"]; !ok {
		t.Errorf("[AC#1] expected key %q inside error object; available keys: %v", "code", mapKeys(errObj))
	}
	if _, ok := errObj["message"]; !ok {
		t.Errorf("[AC#1] expected key %q inside error object; available keys: %v", "message", mapKeys(errObj))
	}
}

// mapKeys returns the keys of a map[string]json.RawMessage for use in test error messages.
func mapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
