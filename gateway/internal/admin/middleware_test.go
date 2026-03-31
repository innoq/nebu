package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootstrapGuard(t *testing.T) {
	tests := []struct {
		name            string
		bootstrapActive bool
		checkerErr      error
		path            string
		wantCode        int
		wantLocation    string
		wantNext        bool
	}{
		{
			name:            "incomplete + non-bootstrap path → redirect to bootstrap",
			bootstrapActive: true,
			checkerErr:      nil,
			path:            "/admin/dashboard",
			wantCode:        http.StatusFound,
			wantLocation:    "/admin/bootstrap",
			wantNext:        false,
		},
		{
			name:            "incomplete + bootstrap path → pass-through",
			bootstrapActive: true,
			checkerErr:      nil,
			path:            "/admin/bootstrap",
			wantCode:        http.StatusOK,
			wantLocation:    "",
			wantNext:        true,
		},
		{
			name:            "complete + bootstrap path → redirect to login",
			bootstrapActive: false,
			checkerErr:      nil,
			path:            "/admin/bootstrap",
			wantCode:        http.StatusFound,
			wantLocation:    "/admin/login",
			wantNext:        false,
		},
		{
			name:            "complete + non-bootstrap path → pass-through",
			bootstrapActive: false,
			checkerErr:      nil,
			path:            "/admin/dashboard",
			wantCode:        http.StatusOK,
			wantLocation:    "",
			wantNext:        true,
		},
		{
			name:            "checker error → 500",
			bootstrapActive: false,
			checkerErr:      errFakeDB,
			path:            "/admin/dashboard",
			wantCode:        http.StatusInternalServerError,
			wantLocation:    "",
			wantNext:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker := &fakeBootstrapChecker{active: tc.bootstrapActive, err: tc.checkerErr}
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})
			handler := BootstrapGuard(checker)(next)

			req := httptest.NewRequest("GET", tc.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rr.Code, tc.wantCode)
			}
			if tc.wantLocation != "" {
				loc := rr.Header().Get("Location")
				if loc != tc.wantLocation {
					t.Errorf("Location: got %q, want %q", loc, tc.wantLocation)
				}
			}
			if tc.wantNext != nextCalled {
				t.Errorf("next called: got %v, want %v", nextCalled, tc.wantNext)
			}
		})
	}
}
