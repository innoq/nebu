package matrix

// ─── Story 7-25: Tags API — GET/PUT/DELETE /user/{userId}/rooms/{roomId}/tags ──
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until tags.go is created and routes are registered.
//
// Acceptance Criteria covered:
//   AC1 — GET /tags → 200 with {"tags":{}} when no tags are set (never 404)
//   AC2 — PUT /tags/{tag} → 200 {} + subsequent GET reflects the tag
//   AC3 — DELETE /tags/{tag} → 200 {} (idempotent: even if tag absent)
//   AC4 — Invalid tag (empty or >100 chars) → 400 M_INVALID_PARAM
//   AC6 — userId in path != authenticated user → 403 M_FORBIDDEN
//
// Design:
//   - Tags reuse the AccountDataDB interface (already defined in account_data.go).
//   - TagsHandler wraps all three verbs; constructor takes TagsConfig.
//   - validateTag is tested via direct export; it lives in tags.go.
//   - The in-memory mockAccountDataDB from account_data_test.go is reused here
//     (same package — Go test files share the package).
//
// NOTE: TagsHandler, TagsConfig, NewTagsHandler, GetTags, PutTag, DeleteTag,
// validateTag are declared in gateway/internal/matrix/tags.go — which does NOT
// exist yet. Every test in this file MUST fail with a compilation error until
// tags.go is created.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedTagsHandler wires JWTMiddleware → TagsHandler and registers all
// three routes on a mux so PathValue resolves correctly.
//
// The JWT subject is always "test-sub-123" → user_id "@test-sub-123:test.local".
// Re-uses mockAccountDataDB from account_data_test.go (same package).
func buildAuthedTagsHandler(t *testing.T, db *mockAccountDataDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewTagsHandler(TagsConfig{
		DB:         db,
		ServerName: "test.local",
	})

	jwt := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags",
		jwt(http.HandlerFunc(handler.GetTags)))
	mux.Handle("PUT /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}",
		jwt(http.HandlerFunc(handler.PutTag)))
	mux.Handle("DELETE /_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}",
		jwt(http.HandlerFunc(handler.DeleteTag)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// authenticatedUserID returns the user_id produced by the mock JWT helper.
// signJWT sets sub=test-sub-123 and name=test-sub-123 → "@test-sub-123:test.local".
const authenticatedUserID = "@test-sub-123:test.local"

// ─── AC1: GET returns {"tags":{}} when no tags are set ───────────────────────

func TestGetTags_EmptyStore_ReturnsEmptyTags(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	rawTags, ok := body["tags"]
	if !ok {
		t.Fatalf("expected 'tags' key in response, got: %s", w.Body.String())
	}
	// Must be an object (not null, not array).
	var tags map[string]json.RawMessage
	if err := json.Unmarshal(rawTags, &tags); err != nil {
		t.Fatalf("'tags' field is not a JSON object: %v; raw: %s", err, rawTags)
	}
	if len(tags) != 0 {
		t.Errorf("expected empty tags object, got %v", tags)
	}
}

// ─── AC1 edge: tags field must never be null ──────────────────────────────────

func TestGetTags_NeverNull(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, `"tags":null`) {
		t.Errorf("'tags' must not be null; body: %s", body)
	}
}

// ─── AC2: PUT stores tag; GET reflects it ────────────────────────────────────

func TestPutTag_SetsFavourite_GetReflects(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()

	// PUT m.favourite with order=0.5
	putBody := `{"order":0.5}`
	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/m.favourite",
		bytes.NewBufferString(putBody))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	putW := httptest.NewRecorder()
	mux.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d; body: %s", putW.Code, putW.Body.String())
	}
	if strings.TrimSpace(putW.Body.String()) != "{}" {
		t.Errorf("PUT: expected body {}, got %q", putW.Body.String())
	}

	// GET — should return m.favourite with order=0.5
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}

	var getBody struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("GET response is not valid JSON: %v; body: %s", err, getW.Body.String())
	}
	tagData, ok := getBody.Tags["m.favourite"]
	if !ok {
		t.Fatalf("expected 'm.favourite' in tags, got: %s", getW.Body.String())
	}

	var tagContent map[string]interface{}
	if err := json.Unmarshal(tagData, &tagContent); err != nil {
		t.Fatalf("tag content is not valid JSON: %v", err)
	}
	order, ok := tagContent["order"]
	if !ok {
		t.Errorf("expected 'order' field in tag, got %v", tagContent)
	}
	if order != 0.5 {
		t.Errorf("expected order=0.5, got %v", order)
	}
}

// ─── AC2: PUT with empty body {} is valid (tag with no order) ────────────────

func TestPutTag_EmptyBody_TagWithNoOrder(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()

	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/u.work",
		bytes.NewBufferString("{}"))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, putReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC3: DELETE removes tag; second DELETE is idempotent (200) ──────────────

func TestDeleteTag_Idempotent(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()

	// Pre-set a tag directly in the store via PutAccountData.
	initialContent := json.RawMessage(`{"tags":{"m.favourite":{"order":0.1}}}`)
	_ = db.PutAccountData(context.Background(),
		authenticatedUserID, "!room1:test.local", "m.tag", initialContent)

	// First DELETE.
	del1 := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/m.favourite", nil)
	del1.Header.Set("Authorization", "Bearer "+token)

	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, del1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first DELETE: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}

	// Second DELETE (tag already gone — must be idempotent).
	del2 := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/m.favourite", nil)
	del2.Header.Set("Authorization", "Bearer "+token)

	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, del2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second DELETE: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}

	// GET must show empty tags.
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, getReq)

	if wGet.Code != http.StatusOK {
		t.Fatalf("GET after DELETE: expected 200, got %d; body: %s", wGet.Code, wGet.Body.String())
	}

	var getBody struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("GET response is not valid JSON: %v", err)
	}
	if len(getBody.Tags) != 0 {
		t.Errorf("expected empty tags after DELETE, got %v", getBody.Tags)
	}
}

// ─── AC4: validateTag helper — empty tag name ─────────────────────────────────

func TestValidateTag_EmptyName(t *testing.T) {
	err := validateTag("")
	if err == nil {
		t.Fatal("expected error for empty tag name, got nil")
	}
}

func TestValidateTag_TooLong(t *testing.T) {
	longTag := strings.Repeat("x", 101)
	err := validateTag(longTag)
	if err == nil {
		t.Fatalf("expected error for tag longer than 100 chars, got nil (len=%d)", len(longTag))
	}
}

func TestValidateTag_MaxLength(t *testing.T) {
	maxTag := strings.Repeat("x", 100)
	err := validateTag(maxTag)
	if err != nil {
		t.Fatalf("expected no error for 100-char tag, got %v", err)
	}
}

// ─── AC4: Invalid tag via HTTP → 400 M_INVALID_PARAM ─────────────────────────

func TestPutTag_InvalidTagName_ViaHTTP(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()

	longTag := strings.Repeat("x", 101)
	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/"+longTag,
		bytes.NewBufferString("{}"))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, putReq)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
}

// ─── AC4: DELETE with invalid tag also returns 400 ───────────────────────────

func TestDeleteTag_InvalidTagName_ViaHTTP(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()

	longTag := strings.Repeat("x", 101)
	delReq := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/"+longTag, nil)
	delReq.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, delReq)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
}

// ─── AC6: userId mismatch → 403 M_FORBIDDEN ──────────────────────────────────

func TestGetTags_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	// JWT sub is "test-sub-123" → "@test-sub-123:test.local"
	// but we request tags for a different user.
	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/@another-user:test.local/rooms/!room1:test.local/tags", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", errResp["errcode"])
	}
}

func TestPutTag_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/user/@bob:test.local/rooms/!room1:test.local/tags/m.favourite",
		bytes.NewBufferString("{}"))
	putReq.Header.Set("Authorization", "Bearer "+makeToken())
	putReq.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, putReq)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", errResp["errcode"])
	}
}

func TestDeleteTag_UserIdMismatch_Forbidden(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	delReq := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/user/@bob:test.local/rooms/!room1:test.local/tags/m.favourite", nil)
	delReq.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, delReq)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %q", errResp["errcode"])
	}
}

// ─── JWT required — no token → 401 ───────────────────────────────────────────

func TestGetTags_Unauthenticated_Returns401(t *testing.T) {
	db := newMockAccountDataDB()
	mux, _ := buildAuthedTagsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags", nil)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── PUT bad JSON body → 400 M_BAD_JSON ──────────────────────────────────────

func TestPutTag_MalformedJSON_Returns400(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)

	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/user/"+authenticatedUserID+"/rooms/!room1:test.local/tags/m.favourite",
		bytes.NewBufferString("not-json"))
	putReq.Header.Set("Authorization", "Bearer "+makeToken())
	putReq.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, putReq)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %q", errResp["errcode"])
	}
}

// ─── Multiple tags — PUT two tags, DELETE one, GET shows remaining ────────────

func TestTags_PutTwo_DeleteOne_GetShowsRemaining(t *testing.T) {
	db := newMockAccountDataDB()
	mux, makeToken := buildAuthedTagsHandler(t, db)
	token := makeToken()
	userPath := "/_matrix/client/v3/user/" + authenticatedUserID + "/rooms/!room1:test.local/tags"

	put := func(tag, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, userPath+"/"+tag, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s: expected 200, got %d; body: %s", tag, w.Code, w.Body.String())
		}
	}

	put("m.favourite", `{"order":0.1}`)
	put("m.lowpriority", `{"order":0.9}`)

	// DELETE m.favourite
	delReq := httptest.NewRequest(http.MethodDelete, userPath+"/m.favourite", nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	wDel := httptest.NewRecorder()
	mux.ServeHTTP(wDel, delReq)
	if wDel.Code != http.StatusOK {
		t.Fatalf("DELETE: expected 200, got %d; body: %s", wDel.Code, wDel.Body.String())
	}

	// GET — only m.lowpriority should remain
	getReq := httptest.NewRequest(http.MethodGet, userPath, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, getReq)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d; body: %s", wGet.Code, wGet.Body.String())
	}

	var getBody struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("GET response is not valid JSON: %v", err)
	}
	if _, ok := getBody.Tags["m.favourite"]; ok {
		t.Error("expected m.favourite to be deleted, but it is still present")
	}
	if _, ok := getBody.Tags["m.lowpriority"]; !ok {
		t.Error("expected m.lowpriority to remain, but it is absent")
	}
}
