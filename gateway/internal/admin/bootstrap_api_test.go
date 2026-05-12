package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
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
	return &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  persister,
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret-for-encryption"),
	}
}

// newTestBootstrapHandlerWithDraftStore creates a BootstrapHandler with both a fake persister and a specific draft store.
func newTestBootstrapHandlerWithDraftStore(t *testing.T, persister BootstrapPersister, draftStore BootstrapDraftStore) *BootstrapHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	return &BootstrapHandler{
		checker:    &fakeBootstrapChecker{active: true},
		tmpl:       tmpl,
		persister:  persister,
		draftStore: draftStore,
		secret:     []byte("test-secret-for-encryption"),
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

// TestStepHandler_Step2_SavesToDraftAndRendersStep3 verifies that after step 2 POST,
// oidc_client_secret is saved (encrypted) to the draft store and the handler
// renders step 3 (Claim Mapping) with pre-filled default values.
// Story 11-10: Step 2 now advances to Step 3 (Claim Mapping) instead of redirecting to OIDC.
// The OIDC redirect happens from Step 3 after claim fields are configured.
func TestStepHandler_Step2_SavesToDraftAndRendersStep3(t *testing.T) {
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

	// Step 2 now renders Step 3 (Claim Mapping) — 200 OK with the step 3 form.
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK (renders step 3), got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `name="oidc_user_id_claim"`) {
		t.Error("after valid step 2, should render step 3 with oidc_user_id_claim field")
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

// TestStepHandler_Step3_ValidClaimsRedirectsToOIDC verifies that step 3 POST with valid
// claim fields saves to draft and redirects to OIDC login (Story 11-10 AC9).
func TestStepHandler_Step3_ValidClaimsRedirectsToOIDC(t *testing.T) {
	persister := &fakeBootstrapPersister{}
	draftStore := &fakeBootstrapDraftStore{}
	handler := newTestBootstrapHandlerWithDraftStore(t, persister, draftStore)

	form := url.Values{}
	form.Set("step", "3")
	form.Set("instance_name", "my-instance")
	form.Set("oidc_issuer", "https://auth.example.com")
	form.Set("oidc_client_id", "nebu-admin")
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	// Step 3 redirects to OIDC login.
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther redirect to OIDC login from step 3, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login/start?mode=bootstrap" {
		t.Errorf("expected redirect to /admin/login/start?mode=bootstrap, got %q", loc)
	}

	// Verify claim fields were saved to draft
	draftStore.mu.Lock()
	_, hasUserID := draftStore.data["oidc_user_id_claim"]
	_, hasDisplayname := draftStore.data["oidc_displayname_claim"]
	_, hasEmail := draftStore.data["oidc_email_claim"]
	draftStore.mu.Unlock()

	if !hasUserID {
		t.Error("expected oidc_user_id_claim to be saved to draft store")
	}
	if !hasDisplayname {
		t.Error("expected oidc_displayname_claim to be saved to draft store")
	}
	if !hasEmail {
		t.Error("expected oidc_email_claim to be saved to draft store")
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
