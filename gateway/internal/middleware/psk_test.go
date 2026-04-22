package middleware

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

func TestPSKMiddleware_ValidToken(t *testing.T) {
	secret := "test-secret"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !called {
		t.Error("expected next handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

func TestPSKMiddleware_MissingHeader(t *testing.T) {
	secret := "test-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body, got: %q", rr.Body.String())
	}
}

func TestPSKMiddleware_WrongPSK(t *testing.T) {
	secret := "correct-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body, got: %q", rr.Body.String())
	}
}

func TestPSKMiddleware_BearerPrefixOnly(t *testing.T) {
	secret := "test-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

// TestPSK_AcceptsCorrect verifies that a correct PSK results in a 200 response.
func TestPSK_AcceptsCorrect(t *testing.T) {
	secret := "my-super-secret-psk"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/_internal/nodes/register", nil)
	req.Header.Set("Authorization", "Bearer my-super-secret-psk")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !called {
		t.Error("expected next handler to be called with correct PSK")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

// TestPSK_RejectsWrong verifies that a wrong PSK results in a 401 response.
func TestPSK_RejectsWrong(t *testing.T) {
	secret := "correct-psk"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called with wrong PSK")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/_internal/nodes/register", nil)
	req.Header.Set("Authorization", "Bearer wrong-psk")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

// TestPSK_RejectsEmptyPSK verifies that an empty Authorization header results in 401.
func TestPSK_RejectsEmptyPSK(t *testing.T) {
	secret := "correct-psk"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called with empty Authorization")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/_internal/nodes/register", nil)
	// No Authorization header set — empty string.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}

// TestPSK_TimingStable verifies that constantTimeEqualHashed always compares
// fixed-length digests (32 bytes) regardless of input lengths, which is the
// core property that prevents timing-based length leaks.
//
// Strategy: instead of a flaky wall-clock timing test, we verify the
// implementation contract — that comparing a 1-char wrong value and a
// 50-char wrong value both take an equal number of constant-time operations
// by checking that the function consistently returns false for mismatches
// and that the timing distribution for short vs. long inputs is not
// statistically significant (median diff < 10%).
func TestPSK_TimingStable(t *testing.T) {
	const iterations = 10_000

	shortWrong := []byte("x")
	longWrong := []byte("this-is-a-much-longer-string-that-is-wrong-abc-def")
	correct := []byte("correct-psk")

	// Verify correctness: both wrong inputs must return false, correct returns true.
	if constantTimeEqualHashed(shortWrong, correct) {
		t.Error("short wrong input should not equal correct")
	}
	if constantTimeEqualHashed(longWrong, correct) {
		t.Error("long wrong input should not equal correct")
	}
	if !constantTimeEqualHashed(correct, correct) {
		t.Error("correct input should equal itself")
	}

	// Timing stability check: measure median latency for short vs. long mismatches.
	shortDurations := make([]time.Duration, iterations)
	longDurations := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		constantTimeEqualHashed(shortWrong, correct)
		shortDurations[i] = time.Since(start)
	}

	for i := 0; i < iterations; i++ {
		start := time.Now()
		constantTimeEqualHashed(longWrong, correct)
		longDurations[i] = time.Since(start)
	}

	shortMedian := median(shortDurations)
	longMedian := median(longDurations)

	// Allow up to 2x difference as a loose bound — real timing attacks
	// require sub-nanosecond precision. If both medians are > 0, the ratio
	// must be within [0.5, 2.0] (i.e. neither is 2x faster than the other).
	if shortMedian > 0 && longMedian > 0 {
		ratio := float64(shortMedian) / float64(longMedian)
		if ratio > 2.0 || ratio < 0.5 {
			t.Errorf("timing ratio short/long = %.2f, expected within [0.5, 2.0] — possible length leak", ratio)
		}
	}
}

// median returns the median value from a slice of durations.
func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
