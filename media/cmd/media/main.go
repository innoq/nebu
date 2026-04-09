package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/nebu/nebu/media/internal/upload"
)

// pgMediaStore is a minimal production implementation of upload.MediaStore.
// It is a placeholder for MVP: logs the insert rather than hitting the DB.
// Story 4-20 will replace this with a real pgx/v5 implementation.
type pgMediaStore struct{}

func (s *pgMediaStore) InsertMediaFile(_ context.Context, row upload.MediaFileRow) error {
	slog.Info("media file recorded",
		"media_id", row.MediaID,
		"server_name", row.ServerName,
		"file_size", row.FileSize,
		"uploader", row.UploaderUserID,
	)
	return nil
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

	store := &pgMediaStore{}

	uploadHandler := upload.NewHandler(upload.HandlerConfig{
		DB:          store,
		StoragePath: storagePath,
		ServerName:  serverName,
		MaxBytes:    maxBytes,
	})

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/media/v3/upload", uploadHandler)
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
