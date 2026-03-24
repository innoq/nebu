package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/nebu/nebu/internal/config"
	"github.com/nebu/nebu/internal/db"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	"github.com/nebu/nebu/internal/health"
	"github.com/nebu/nebu/internal/middleware"
	"github.com/nebu/nebu/internal/registry"
)

func main() {
	slog.Info("Nebu Gateway starting")

	cfg := config.Load()

	if cfg.DBURL == "" {
		slog.Error("database configuration required", "error", "NEBU_DB_URL not set")
		os.Exit(1)
	}

	if err := db.RunMigrations(cfg.DBURL); err != nil {
		slog.Error("database connection failed: " + err.Error())
		os.Exit(1)
	}

	slog.Info("migrations complete")

	serverName, err := db.InitServerConfig(cfg.DBURL, cfg.ServerName)
	if err != nil {
		slog.Error("server config initialization failed: " + err.Error())
		os.Exit(1)
	}
	if serverName != "" {
		slog.Info("Gateway using server name", "server_name", serverName)
	}

	coreClient, err := coregrpc.New(cfg.CoreGRPCAddr)
	if err != nil {
		slog.Error("failed to create gRPC client", "err", err)
		os.Exit(1)
	}
	defer coreClient.Close()
	slog.Info("gRPC client initialized", "addr", cfg.CoreGRPCAddr)

	// Public HTTP server on :8080 (health, readiness — no auth)
	pubMux := http.NewServeMux()
	healthHandler := health.NewHandler(cfg.DBURL, coreClient)
	pubMux.HandleFunc("GET /health", healthHandler.Health)
	pubMux.HandleFunc("GET /ready", healthHandler.Ready)

	go func() {
		slog.Info("Public HTTP server starting", "addr", ":8080")
		if err := http.ListenAndServe(":8080", pubMux); err != nil {
			slog.Error("Public HTTP server failed", "err", err)
			os.Exit(1)
		}
	}()

	// Read PSK from file once at startup
	pskBytes, err := os.ReadFile(cfg.InternalSecretFile)
	if err != nil {
		slog.Error("failed to read internal secret file", "path", cfg.InternalSecretFile, "err", err)
		os.Exit(1)
	}
	internalSecret := strings.TrimSpace(string(pskBytes))

	// Set up HTTP mux with node registry behind PSK middleware
	mux := http.NewServeMux()
	reg := registry.New()
	regHandler := registry.NewHandler(reg)
	pskHandler := middleware.PSKMiddleware(internalSecret)(regHandler)

	mux.Handle("POST /internal/nodes/register", pskHandler)
	mux.Handle("GET /internal/nodes", pskHandler)

	slog.Info("HTTP server starting", "addr", ":8008")
	if err := http.ListenAndServe(":8008", mux); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}
