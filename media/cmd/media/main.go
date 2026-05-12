package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebu/nebu/media/internal/download"
	"github.com/nebu/nebu/media/internal/storage"
	"github.com/nebu/nebu/media/internal/upload"
)

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

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := &pgMediaStore{pool: pool}

	localStorer := &storage.LocalStorer{BasePath: storagePath}

	uploadHandler := upload.NewHandler(upload.HandlerConfig{
		DB:         store,
		Storage:    localStorer,
		ServerName: serverName,
		MaxBytes:   maxBytes,
	})

	downloadHandler := download.NewHandler(download.HandlerConfig{
		DB:      store,
		Storage: localStorer,
	})

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", uploadHandler)
	mux.Handle("GET /_matrix/media/v3/download/{serverName}/{mediaId}", downloadHandler)
	mux.HandleFunc("GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}", thumbnailStub)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	slog.Info("Nebu Media Gateway listening", "addr", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func getenv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// thumbnailStub returns 501 M_UNRECOGNIZED for all thumbnail requests.
// Thumbnails are Phase 2; the endpoint is registered to avoid 404 confusion.
func thumbnailStub(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"errcode": "M_UNRECOGNIZED",
		"error":   "Thumbnails not supported in this version",
	})
}
