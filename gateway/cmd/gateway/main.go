package main

import (
	"log/slog"
	"os"

	"github.com/nebu/nebu/internal/config"
	"github.com/nebu/nebu/internal/db"
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

	// HTTP listener started in Story 1.11
}
