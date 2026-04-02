package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBootstrapPersister is a test double for BootstrapPersister.
type fakeBootstrapPersister struct {
	err         error
	savedConfig *fakeBootstrapConfig
	callCount   int
}

type fakeBootstrapConfig struct {
	instanceName    string
	oidcIssuer      string
	oidcClientID    string
	encryptedSecret string
}

func (f *fakeBootstrapPersister) SaveBootstrapConfig(_ context.Context, instanceName, oidcIssuer, oidcClientID, encryptedSecret string) error {
	f.callCount++
	if f.err != nil {
		return f.err
	}
	f.savedConfig = &fakeBootstrapConfig{
		instanceName:    instanceName,
		oidcIssuer:      oidcIssuer,
		oidcClientID:    oidcClientID,
		encryptedSecret: encryptedSecret,
	}
	return nil
}

// fakeBootstrapDraftStore is a test double for BootstrapDraftStore.
type fakeBootstrapDraftStore struct {
	mu         sync.Mutex
	data       map[string]string
	fail       bool
	clearCount int
}

func (f *fakeBootstrapDraftStore) SaveDraft(_ context.Context, key, value string) error {
	if f.fail {
		return errors.New("fake draft store error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data == nil {
		f.data = make(map[string]string)
	}
	f.data[key] = value
	return nil
}

func (f *fakeBootstrapDraftStore) LoadDraft(_ context.Context, key string) (string, bool, error) {
	if f.fail {
		return "", false, errors.New("fake draft store error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeBootstrapDraftStore) ClearDraft(_ context.Context) error {
	if f.fail {
		return errors.New("fake draft store error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCount++
	f.data = make(map[string]string)
	return nil
}

// newTestBootstrapHandlerWithPersister creates a BootstrapHandler with a fake persister and draft store for API tests.
func newTestBootstrapHandlerWithPersister(t *testing.T, persister BootstrapPersister) *BootstrapHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  persister,
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret-for-encryption"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	return h
}

// newTestBootstrapHandlerWithDraftStore creates a BootstrapHandler with both a fake persister and a specific draft store.
func newTestBootstrapHandlerWithDraftStore(t *testing.T, persister BootstrapPersister, draftStore BootstrapDraftStore) *BootstrapHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  persister,
		draftStore: draftStore,
		secret:     []byte("test-secret-for-encryption"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	return h
}

// TestFinalizeHandler_Success verifies step 4 with valid fields and a pre-seeded
// draft store results in a 303 redirect to /admin/login.
func TestFinalizeHandler_Success(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	// Pre-seed the OIDC client secret into the draft store (simulates step 2 submission).
	// Encrypt the secret as step 2 would do.
	encSecret, err := encryptAES256GCM(handler.secret, "s3cr3t")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}
	if err := draftStore.SaveDraft(t.Context(), "oidc_client_secret", encSecret); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	form := url.Values{}
	form.Set("step", "4")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	// oidc_client_secret intentionally NOT in form — read from draft store

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.FinalizeHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login/start?mode=bootstrap" {
		t.Errorf("expected redirect to /admin/login/start?mode=bootstrap, got %q", loc)
	}
	if persister.callCount != 1 {
		t.Errorf("expected persister to be called once, got %d", persister.callCount)
	}
	if persister.savedConfig == nil {
		t.Fatal("expected config to be saved")
	}
	if persister.savedConfig.instanceName != "my-instance" {
		t.Errorf("expected instanceName=my-instance, got %q", persister.savedConfig.instanceName)
	}
	if persister.savedConfig.oidcIssuer != "https://auth.example.com" {
		t.Errorf("expected oidcIssuer, got %q", persister.savedConfig.oidcIssuer)
	}
	if persister.savedConfig.oidcClientID != "nebu-admin" {
		t.Errorf("expected oidcClientID, got %q", persister.savedConfig.oidcClientID)
	}
	// Verify secret is encrypted (not equal to plaintext)
	if persister.savedConfig.encryptedSecret == "s3cr3t" {
		t.Error("expected encrypted secret, got plaintext")
	}
	if persister.savedConfig.encryptedSecret == "" {
		t.Error("expected non-empty encrypted secret")
	}
}

// TestFinalizeHandler_MissingDraftSecret verifies that step 4 with an empty draft store
// renders a "Session data missing" error and returns 422.
func TestFinalizeHandler_MissingDraftSecret(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)
	// draftStore is empty (no pre-seeded secret)

	form := url.Values{}
	form.Set("step", "4")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.FinalizeHandler(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 UnprocessableEntity, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Session data missing") {
		t.Error("expected 'Session data missing' error message in response body")
	}
	if persister.callCount != 0 {
		t.Errorf("expected persister NOT to be called, got %d", persister.callCount)
	}
}

// TestMaskSecret verifies maskSecret returns correct masked formats.
func TestMaskSecret(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcdefghij", "abc...hij"},
		{"abc123", "abc...123"},
		{"short", "***"},
		{"", "***"},
		{"12345", "***"},
	}
	for _, tc := range tests {
		got := maskSecret(tc.input)
		if got != tc.want {
			t.Errorf("maskSecret(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestStepHandler_Step3_MaskedSecret verifies that advancing from step 3 to step 4
// via StepHandler includes a masked secret in the rendered HTML when draft store has secret.
func TestStepHandler_Step3_MaskedSecret(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	// Pre-seed an encrypted secret in the draft store (simulates step 2 result)
	encSecret, err := encryptAES256GCM(handler.secret, "supersecretvalue")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}
	if err := draftStore.SaveDraft(t.Context(), "oidc_client_secret", encSecret); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	form := url.Values{}
	form.Set("step", "3")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	// maskSecret("supersecretvalue") = "sup...lue"
	if !strings.Contains(body, "sup...lue") {
		excerpt := body
		if len(excerpt) > 300 {
			excerpt = excerpt[:300]
		}
		t.Errorf("expected masked secret 'sup...lue' in step 4 render, body excerpt: %s", excerpt)
	}
}

// TestFinalizeHandler_ValidationError verifies step 4 with invalid instance_name returns 422.
func TestFinalizeHandler_ValidationError(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	// Pre-seed the secret so instance_name validation is the only failure
	encSecret, err := encryptAES256GCM(handler.secret, "s3cr3t")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}
	if err := draftStore.SaveDraft(t.Context(), "oidc_client_secret", encSecret); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	form := url.Values{}
	form.Set("step", "4")
	form.Set("instance_name", "ab") // too short — will fail validation
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	// oidc_client_secret NOT in form — read from draft store

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.FinalizeHandler(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "3–64") {
		t.Error("expected error message referencing 3–64 character limit")
	}
	if persister.callCount != 0 {
		t.Errorf("expected persister NOT to be called, got %d", persister.callCount)
	}
}

// TestFinalizeHandler_DBError verifies step 4 with DB error returns 500 and re-renders step 4
// including the global error message.
func TestFinalizeHandler_DBError(t *testing.T) {
	persister := &fakeBootstrapPersister{err: errFakeDB}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	// Pre-seed the OIDC client secret into the draft store
	encSecret, err := encryptAES256GCM(handler.secret, "s3cr3t")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}
	if err := draftStore.SaveDraft(t.Context(), "oidc_client_secret", encSecret); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	form := url.Values{}
	form.Set("step", "4")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	// oidc_client_secret NOT in form — read from draft store

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.FinalizeHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Step 4") {
		t.Error("expected step 4 re-render in response body")
	}
	// Verify the global error is now rendered (MAJOR-2 fix)
	if !strings.Contains(body, "Failed to save configuration") {
		t.Error("expected global error message in step 4 re-render")
	}
}

// TestStepHandler_Step2_SavesToDraft verifies that after step 2 POST,
// oidc_client_secret is saved (encrypted) to the draft store.
func TestStepHandler_Step2_SavesToDraft(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	form := url.Values{}
	form.Set("step", "2")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	form.Set("oidc_client_secret", "plaintext-secret-value")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Verify the draft store received an encrypted (non-plaintext) secret
	draftStore.mu.Lock()
	savedEncSecret, ok := draftStore.data["oidc_client_secret"]
	draftStore.mu.Unlock()

	if !ok {
		t.Fatal("expected oidc_client_secret to be saved to draft store")
	}
	if savedEncSecret == "plaintext-secret-value" {
		t.Error("expected encrypted secret in draft store, got plaintext")
	}
	if savedEncSecret == "" {
		t.Error("expected non-empty encrypted secret in draft store")
	}

	// Verify that the saved value can be decrypted back to the original
	decrypted, err := decryptAES256GCM(handler.secret, savedEncSecret)
	if err != nil {
		t.Fatalf("decryptAES256GCM failed: %v", err)
	}
	if decrypted != "plaintext-secret-value" {
		t.Errorf("expected decrypted value = 'plaintext-secret-value', got %q", decrypted)
	}

	// Verify other OIDC fields were saved too
	draftStore.mu.Lock()
	_, hasIssuer := draftStore.data["oidc_issuer"]
	_, hasClientID := draftStore.data["oidc_client_id"]
	draftStore.mu.Unlock()

	if !hasIssuer {
		t.Error("expected oidc_issuer to be saved to draft store")
	}
	if !hasClientID {
		t.Error("expected oidc_client_id to be saved to draft store")
	}
}

// TestFinalizeHandler_ClearsDraftOnSuccess verifies ClearDraft is called
// after successful finalization.
func TestFinalizeHandler_ClearsDraftOnSuccess(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	// Pre-seed the encrypted secret
	encSecret, err := encryptAES256GCM(handler.secret, "s3cr3t")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}
	if err := draftStore.SaveDraft(t.Context(), "oidc_client_secret", encSecret); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	form := url.Values{}
	form.Set("step", "4")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.FinalizeHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", rr.Code)
	}

	// Verify ClearDraft was called
	draftStore.mu.Lock()
	clearCount := draftStore.clearCount
	draftStore.mu.Unlock()

	if clearCount != 1 {
		t.Errorf("expected ClearDraft to be called once, got %d", clearCount)
	}

	// Verify the draft store is now empty
	draftStore.mu.Lock()
	dataLen := len(draftStore.data)
	draftStore.mu.Unlock()

	if dataLen != 0 {
		t.Errorf("expected draft store to be empty after finalization, got %d entries", dataLen)
	}
}

// TestTestOIDCHandler_MissingIssuer verifies empty oidc_issuer returns ok:false.
func TestTestOIDCHandler_MissingIssuer(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)

	form := url.Values{}
	// oidc_issuer intentionally omitted

	req := httptest.NewRequest("POST", "/admin/bootstrap/test-oidc", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.TestOIDCHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp testOIDCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.OK {
		t.Error("expected ok:false for missing issuer")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

// TestTestOIDCHandler_InvalidURL verifies non-HTTPS URL returns ok:false.
func TestTestOIDCHandler_InvalidURL(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)

	form := url.Values{}
	form.Set("oidc_issuer", "http://insecure.example.com")

	req := httptest.NewRequest("POST", "/admin/bootstrap/test-oidc", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.TestOIDCHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp testOIDCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.OK {
		t.Error("expected ok:false for non-HTTPS URL")
	}
}

// TestTestOIDCHandler_DiscoverySuccess verifies valid issuer with 200 stub returns ok:true.
func TestTestOIDCHandler_DiscoverySuccess(t *testing.T) {
	// Stub OIDC discovery server
	stubServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stubServer.Close()

	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)
	// Use the stub server's HTTP client (trusts the test TLS cert)
	handler.httpClient = stubServer.Client()

	form := url.Values{}
	form.Set("oidc_issuer", stubServer.URL)

	req := httptest.NewRequest("POST", "/admin/bootstrap/test-oidc", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.TestOIDCHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp testOIDCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected ok:true, got ok:false error=%q", resp.Error)
	}
}

// TestTestOIDCHandler_DiscoveryFailure verifies valid issuer with 503 stub returns ok:false.
func TestTestOIDCHandler_DiscoveryFailure(t *testing.T) {
	// Stub OIDC discovery server returning 503
	stubServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer stubServer.Close()

	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)
	handler.httpClient = stubServer.Client()

	form := url.Values{}
	form.Set("oidc_issuer", stubServer.URL)

	req := httptest.NewRequest("POST", "/admin/bootstrap/test-oidc", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.TestOIDCHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp testOIDCResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.OK {
		t.Error("expected ok:false for 503 response")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

// TestGenerateKeysHandler_Success verifies POST returns 200 with ok:true and non-empty fingerprint.
func TestGenerateKeysHandler_Success(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	handler := newTestBootstrapHandlerWithPersister(t, persister)

	req := httptest.NewRequest("POST", "/admin/bootstrap/generate-keys", nil)
	rr := httptest.NewRecorder()
	handler.GenerateKeysHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp generateKeysResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if !resp.OK {
		t.Error("expected ok:true in generate-keys response")
	}
	if resp.Ed25519PublicFingerprint == "" {
		t.Error("expected non-empty ed25519_public_fingerprint")
	}
	// Fingerprint should be 16 hex characters (8 bytes)
	if len(resp.Ed25519PublicFingerprint) != 16 {
		t.Errorf("expected fingerprint length 16, got %d", len(resp.Ed25519PublicFingerprint))
	}
}

// TestEncryptDecrypt_RoundTrip verifies encrypt then decrypt recovers original.
func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	secret := []byte("my-test-secret-for-aes-key-derivation")
	plaintext := "super-secret-oidc-client-secret"

	encrypted, err := encryptAES256GCM(secret, plaintext)
	if err != nil {
		t.Fatalf("encryptAES256GCM failed: %v", err)
	}
	if encrypted == plaintext {
		t.Error("encrypted text must not equal plaintext")
	}
	if encrypted == "" {
		t.Error("encrypted text must not be empty")
	}

	decrypted, err := decryptAES256GCM(secret, encrypted)
	if err != nil {
		t.Fatalf("decryptAES256GCM failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}
