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
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/thumbnail"
	"github.com/nebu/nebu/media/internal/upload"
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

	serverName := getenv("NEBU_SERVER_NAME", "localhost")
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

	// HIGH-2 [Story 12.7]: OIDC JWT verification for media upload.
	// If NEBU_OIDC_ISSUER is set, create a verifier that validates upload tokens
	// against the same OIDC provider as the API gateway.
	// If not configured, the upload handler falls back to MVP bearer-presence check.
	var uploadVerifier upload.TokenVerifier
	if oidcIssuer := getenv("NEBU_OIDC_ISSUER", ""); oidcIssuer != "" {
		oidcClientID := getenv("NEBU_OIDC_CLIENT_ID", "nebu")
		oidcProvider, err := oidc.NewProvider(ctx, oidcIssuer)
		if err != nil {
			slog.Warn("media: OIDC provider unavailable — upload JWT validation disabled until resolved",
				"issuer", oidcIssuer, "err", err)
			// Fail-open: allow startup without OIDC (provider may come up after media).
			// The handler will fall back to MVP bearer-presence check.
		} else {
			uploadVerifier = oidcProvider.Verifier(&oidc.Config{ClientID: oidcClientID})
			slog.Info("media: OIDC JWT validation enabled for uploads", "issuer", oidcIssuer)
		}
	} else {
		slog.Warn("media: NEBU_OIDC_ISSUER not set — upload JWT validation disabled (MVP mode)")
	}

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

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", uploadHandler)
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", downloadHandler)
	mux.Handle("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", thumbnailHandler)
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

