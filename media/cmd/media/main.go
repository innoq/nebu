package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/nebu/nebu/media/internal/download"
	"github.com/nebu/nebu/media/internal/ratelimit"
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/thumbnail"
	"github.com/nebu/nebu/media/internal/upload"
	"golang.org/x/time/rate"
)

// mediaConfig holds all configuration values for the media gateway.
// Populated from environment variables in main(); passed to selectStorer.
type mediaConfig struct {
	serverName     string
	storagePath    string
	listenAddr     string
	maxBytes       int64
	dbURL          string
	storageBackend string // "local" (default) or "minio"

	// MinIO-specific (only used when storageBackend == "minio")
	minioEndpoint  string
	minioAccessKey string
	minioSecretKey string
	minioBucket    string
	minioUseSSL    bool
}

// pgMediaStore implements upload.MediaStore and download.MediaStore using pgx/v5.
// AC #7 — Real pgx/v5 DB layer replacing the placeholder stub.
type pgMediaStore struct {
	pool *pgxpool.Pool
}

// InsertMediaFile inserts a row into media_files.
func (s *pgMediaStore) InsertMediaFile(ctx context.Context, row upload.MediaFileRow) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO media_files
		 (media_id, server_name, content_type, file_size, aes_key_hex, nonce_hex, uploader_user_id, uploaded_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		row.MediaID, row.ServerName, row.ContentType, row.FileSize,
		row.AESKeyHex, row.NonceHex, row.UploaderUserID, row.UploadedAt,
	)
	return err
}

// GetMediaFile fetches a row from media_files by server_name + media_id.
// Returns nil, nil if no row found (caller must check for nil).
func (s *pgMediaStore) GetMediaFile(ctx context.Context, serverName, mediaID string) (*download.MediaFileRow, error) {
	row := &download.MediaFileRow{}
	err := s.pool.QueryRow(ctx,
		`SELECT media_id, server_name, content_type, aes_key_hex, nonce_hex
		 FROM media_files WHERE server_name = $1 AND media_id = $2 AND NOT deleted`,
		serverName, mediaID,
	).Scan(&row.MediaID, &row.ServerName, &row.ContentType, &row.AESKeyHex, &row.NonceHex)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // not found → caller returns 404
		}
		return nil, err
	}
	return row, nil
}

// pgThumbnailStore is an adapter that wraps pgMediaStore to satisfy thumbnail.MediaStore.
// Go does not allow the same method name to return different types in the same struct,
// so this thin adapter converts download.MediaFileRow to thumbnail.MediaFileRow.
type pgThumbnailStore struct {
	inner *pgMediaStore
}

// GetMediaFile implements thumbnail.MediaStore by delegating to the underlying pgMediaStore
// and converting the returned row type.
func (s *pgThumbnailStore) GetMediaFile(ctx context.Context, serverName, mediaID string) (*thumbnail.MediaFileRow, error) {
	row, err := s.inner.GetMediaFile(ctx, serverName, mediaID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &thumbnail.MediaFileRow{
		MediaID:     row.MediaID,
		ServerName:  row.ServerName,
		ContentType: row.ContentType,
		AESKeyHex:   row.AESKeyHex,
		NonceHex:    row.NonceHex,
	}, nil
}

func main() {
	slog.Info("Nebu Media Gateway starting")

	// Story 12.9 — NEBU_SERVER_NAME is mandatory (AC-3).
	// Without it, uploader_user_id would contain a wrong server part
	// in the canonical Matrix user ID (@localpart:server).
	serverName := os.Getenv("NEBU_SERVER_NAME")
	if serverName == "" {
		slog.Error("FATAL: NEBU_SERVER_NAME is required")
		os.Exit(1)
	}
	storagePath := getenv("NEBU_MEDIA_STORAGE_PATH", "/var/nebu/media")
	listenAddr := getenv("NEBU_MEDIA_LISTEN_ADDR", ":8009")
	maxBytesStr := getenv("NEBU_MEDIA_MAX_UPLOAD_BYTES", "52428800")

	maxBytes, err := strconv.ParseInt(maxBytesStr, 10, 64)
	if err != nil {
		slog.Error("invalid NEBU_MEDIA_MAX_UPLOAD_BYTES", "value", maxBytesStr, "err", err)
		os.Exit(1)
	}

	dbURL := os.Getenv("NEBU_DB_URL")
	if dbURL == "" {
		slog.Error("NEBU_DB_URL is required")
		os.Exit(1)
	}

	// Build MinIO credentials: _FILE form (Docker Secrets) takes priority over plain env var.
	// LOW-9 [Story 12.7]: Reversed precedence — file takes priority. This aligns with the
	// gateway's NEBU_INTERNAL_SECRET_FILE pattern and prevents accidental .env file commits
	// from silently overriding the intended Docker Secrets configuration.
	//
	// Resolution order:
	//   1. NEBU_MINIO_ACCESS_KEY_FILE — read from Docker Secret file (preferred)
	//   2. NEBU_MINIO_ACCESS_KEY — plain environment variable (fallback for dev)
	var minioAccessKey string
	if keyFile := getenv("NEBU_MINIO_ACCESS_KEY_FILE", ""); keyFile != "" {
		minioAccessKey, err = readSecretFile(keyFile)
		if err != nil {
			slog.Error("failed to read NEBU_MINIO_ACCESS_KEY_FILE", "path", keyFile, "err", err)
			os.Exit(1)
		}
	} else {
		minioAccessKey = getenv("NEBU_MINIO_ACCESS_KEY", "")
	}

	var minioSecretKey string
	if secretFile := getenv("NEBU_MINIO_SECRET_KEY_FILE", ""); secretFile != "" {
		minioSecretKey, err = readSecretFile(secretFile)
		if err != nil {
			slog.Error("failed to read NEBU_MINIO_SECRET_KEY_FILE", "path", secretFile, "err", err)
			os.Exit(1)
		}
	} else {
		minioSecretKey = getenv("NEBU_MINIO_SECRET_KEY", "")
	}

	minioUseSSL := false
	if v := getenv("NEBU_MINIO_USE_SSL", "false"); v == "true" || v == "1" {
		minioUseSSL = true
	}

	cfg := mediaConfig{
		serverName:     serverName,
		storagePath:    storagePath,
		listenAddr:     listenAddr,
		maxBytes:       maxBytes,
		dbURL:          dbURL,
		storageBackend: getenv("NEBU_STORAGE_BACKEND", "local"),
		minioEndpoint:  getenv("NEBU_MINIO_ENDPOINT", ""),
		minioAccessKey: minioAccessKey,
		minioSecretKey: minioSecretKey,
		minioBucket:    getenv("NEBU_MINIO_BUCKET", "nebu-media"),
		minioUseSSL:    minioUseSSL,
	}

	storer, err := selectStorer(cfg)
	if err != nil {
		slog.Error("failed to initialise storage backend", "backend", cfg.storageBackend, "err", err)
		os.Exit(1)
	}

	slog.Info("storage backend initialised", "backend", cfg.storageBackend)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Story 12.8 — Fail-closed OIDC startup.
	// NEBU_OIDC_ISSUER is mandatory. An empty value causes a fatal exit (AC-1).
	// If Dex is unreachable, initOIDCVerifier retries up to 5 times with 2s backoff
	// and then calls os.Exit(1) (AC-2). On success it returns a non-nil verifier (AC-3).
	oidcIssuer := os.Getenv("NEBU_OIDC_ISSUER")
	if oidcIssuer == "" {
		slog.Error("FATAL: NEBU_OIDC_ISSUER is required")
		os.Exit(1)
	}
	oidcClientID := getenv("NEBU_OIDC_CLIENT_ID", "nebu")

	// Story 12.11 — SEC Fix F-1: NEBU_OIDC_USER_ID_CLAIM configures which OIDC
	// claim is used as the user identity in audit records (media_files.uploader_user_id).
	// Default "name" matches migration 000044 column comment and the gateway DB default.
	oidcUserIDClaim := getenv("NEBU_OIDC_USER_ID_CLAIM", "name")

	uploadVerifier := initOIDCVerifier(ctx, oidcIssuer, oidcClientID, oidcUserIDClaim, 5, 2*time.Second)

	store := &pgMediaStore{pool: pool}
	thumbStore := &pgThumbnailStore{inner: store}

	uploadHandler := upload.NewHandler(upload.HandlerConfig{
		DB:           store,
		Storage:      storer,
		ServerName:   serverName,
		MaxBytes:     maxBytes,
		OIDCVerifier: uploadVerifier,
	})

	downloadHandler := download.NewHandler(download.HandlerConfig{
		DB:      store,
		Storage: storer,
	})

	thumbnailHandler := thumbnail.NewHandler(thumbnail.HandlerConfig{
		DB:      thumbStore,
		Storage: storer,
	})

	// Story 12.10 — Per-IP Rate Limiting.
	// Story 12.11 — SEC Fix F-2: NEBU_TRUSTED_PROXY gates XFF extraction.
	//   false (default): always key on RemoteAddr — XFF spoofing bypass not possible.
	//   true: use rightmost XFF entry — only set when behind a trusted reverse proxy.
	// Story 12.12 — F-5: Emit a single startup warning when rate limiting is disabled.
	//   Exactly one log line at startup — not per-request, not per-limiter-instance.
	//   Allows operators to verify via startup logs whether rate limiting is active.
	// Upload tier: 10 req/s per IP (burst 5).
	// Download/thumbnail tier: 100 req/s per IP (burst 20).
	// NEBU_RATE_LIMIT_DISABLED=true disables both tiers (dev/test escape hatch).
	logIfRateLimitDisabled()
	trustedProxy := os.Getenv("NEBU_TRUSTED_PROXY") == "true"
	uploadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{
		Rate:  rate.Limit(10),
		Burst: 5,
	}, trustedProxy)
	downloadRL := ratelimit.NewIPRateLimiter(ratelimit.Config{
		Rate:  rate.Limit(100),
		Burst: 20,
	}, trustedProxy)

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", uploadRL(uploadHandler))
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", downloadRL(downloadHandler))
	mux.Handle("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", downloadRL(thumbnailHandler))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// LOW-10 [Story 12.7]: Use http.Server with explicit timeouts to prevent Slowloris attacks.
	// Without these, a client holding a TCP connection open with dribbled header bytes
	// can exhaust goroutines/file descriptors on the newly-exposed :8009 port.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	slog.Info("Nebu Media Gateway listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// selectStorer returns the appropriate Storer implementation based on cfg.storageBackend.
// "minio" requires cfg.minioEndpoint to be set; returns an error if it is empty.
// Any other value (including empty) defaults to LocalStorer.
func selectStorer(cfg mediaConfig) (storage.Storer, error) {
	switch cfg.storageBackend {
	case "minio":
		if cfg.minioEndpoint == "" {
			return nil, fmt.Errorf("NEBU_MINIO_ENDPOINT required when NEBU_STORAGE_BACKEND=minio")
		}
		if cfg.minioAccessKey == "" {
			return nil, fmt.Errorf("NEBU_MINIO_ACCESS_KEY (or NEBU_MINIO_ACCESS_KEY_FILE) required when NEBU_STORAGE_BACKEND=minio")
		}
		if cfg.minioSecretKey == "" {
			return nil, fmt.Errorf("NEBU_MINIO_SECRET_KEY (or NEBU_MINIO_SECRET_KEY_FILE) required when NEBU_STORAGE_BACKEND=minio")
		}
		client, err := minio.New(cfg.minioEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.minioAccessKey, cfg.minioSecretKey, ""),
			Secure: cfg.minioUseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("minio client init: %w", err)
		}
		return &storage.MinIOStorer{Client: client, Bucket: cfg.minioBucket}, nil
	default: // "local" or empty string
		return &storage.LocalStorer{BasePath: cfg.storagePath}, nil
	}
}

// readSecretFile reads a Docker Secret file (or any file) and returns its trimmed contents.
// This mirrors the gateway pattern for NEBU_INTERNAL_SECRET_FILE.
func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret file %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func getenv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// logIfRateLimitDisabled emits a single slog.Warn at startup when
// NEBU_RATE_LIMIT_DISABLED=true, so operators can verify via startup logs
// whether rate limiting is active. Exactly one call per startup.
// Story 12.12 — F-5 (AC-F5-1, AC-F5-2).
func logIfRateLimitDisabled() {
	if os.Getenv("NEBU_RATE_LIMIT_DISABLED") == "true" {
		slog.Warn("rate limiting disabled — NEBU_RATE_LIMIT_DISABLED is set")
	}
}

// initOIDCVerifier initialises an OIDC token verifier for the given issuer.
// It retries up to maxAttempts times, sleeping retryDelay between attempts.
// Each attempt is bounded by a 10-second per-attempt timeout so a hung OIDC
// provider (accepts TCP but never responds) cannot block startup indefinitely.
// If all attempts fail, it logs a FATAL error and calls os.Exit(1) — the service
// must not start without a working OIDC verifier (fail-closed, Story 12.8).
//
// Story 12.12 — F-3: parent ctx cancellation (e.g. SIGTERM during startup)
// propagates immediately into the retry loop, causing early exit.
//
// claimName is the OIDC claim used as the uploader identity (Story 12.11, AC-F1-3).
// An empty issuer triggers an immediate fatal exit (AC-1).
// On success it returns a non-nil upload.TokenVerifier (AC-3).
func initOIDCVerifier(ctx context.Context, issuer, clientID, claimName string, maxAttempts int, retryDelay time.Duration) upload.TokenVerifier {
	if issuer == "" {
		slog.Error("FATAL: NEBU_OIDC_ISSUER is required")
		os.Exit(1)
		return nil // unreachable
	}
	v, _, err := initOIDCVerifierWith(ctx, issuer, clientID, claimName, maxAttempts, retryDelay, 10*time.Second, oidc.NewProvider)
	if err != nil {
		slog.Error("FATAL: media: OIDC provider unreachable after retries",
			"issuer", issuer, "attempts", maxAttempts, "err", err)
		os.Exit(1)
		return nil // unreachable
	}
	return v
}

// oidcNewProviderFunc is the function type for constructing an OIDC provider.
// It matches the signature of oidc.NewProvider and is used by initOIDCVerifierWith.
type oidcNewProviderFunc func(ctx context.Context, issuer string) (*oidc.Provider, error)

// initOIDCVerifierWith is the testable variant of initOIDCVerifier.
// It accepts an injectable newProvider function — the real path passes oidc.NewProvider;
// tests inject a mock that returns controlled errors to verify retry behavior.
//
// Story 12.12 — F-3: Per-attempt timeout (AC-F3-1..3):
//   - attemptTimeout bounds each newProvider call. A hung provider (accepts TCP,
//     never responds) cannot block for more than attemptTimeout per attempt.
//   - Parent ctx cancellation is checked at the top of every retry iteration.
//     If ctx is cancelled (e.g. SIGTERM during startup), the loop exits immediately
//     without waiting for the per-attempt timeout to expire.
//   - Production callers pass 10*time.Second as attemptTimeout.
//     Tests pass short timeouts (e.g. 50ms) for fast iteration.
//
// claimName is the OIDC claim used as the uploader identity (Story 12.11).
// Returns (verifier, attemptCount, lastErr).
// When all attempts are exhausted, returns (nil, maxAttempts, lastErr).
// Callers (initOIDCVerifier) are responsible for calling os.Exit on non-nil err.
func initOIDCVerifierWith(
	ctx context.Context,
	issuer, clientID, claimName string,
	maxAttempts int,
	retryDelay time.Duration,
	attemptTimeout time.Duration,
	newProvider oidcNewProviderFunc,
) (upload.TokenVerifier, int, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// F-3 AC-F3-3: check parent context cancellation before each attempt.
		// Exits immediately on SIGTERM or other cancellation without waiting
		// for the per-attempt timeout to expire.
		if ctx.Err() != nil {
			return nil, attempt - 1, ctx.Err()
		}

		// F-3 AC-F3-1: per-attempt timeout prevents indefinite blocking on hung providers.
		// Each attempt has its own deadline; the parent ctx carries the SIGTERM cancellation.
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		provider, err := newProvider(attemptCtx, issuer)
		cancel()

		if err == nil {
			idTokenVerifier := provider.Verifier(&oidc.Config{ClientID: clientID})
			verifier := upload.NewOIDCTokenVerifier(idTokenVerifier, claimName)
			slog.Info("media: OIDC verifier initialised", "issuer", issuer, "attempt", attempt,
				"oidc_claim", claimName)
			return verifier, attempt, nil
		}
		lastErr = err
		slog.Warn("media: OIDC provider unavailable, retrying",
			"attempt", attempt, "max", maxAttempts, "err", lastErr)
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}
	return nil, maxAttempts, lastErr
}

