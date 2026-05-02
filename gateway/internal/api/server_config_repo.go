//go:build go1.22

// Package api provides the ServerConfigRepository interface and its PostgreSQL
// implementation for the server config Admin API (Story 6.10).
package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ServerConfigRepository abstracts database access for server-wide configuration keys.
// The interface is defined here so that unit tests can provide a mock implementation
// without a real PostgreSQL connection.
//
// GetServerConfig reads the readable config keys from server_config (excludes oidc_client_secret).
// UpsertServerConfigKey upserts a single key-value pair using ON CONFLICT DO UPDATE.
//
// SECURITY: oidc_client_secret is NOT part of ServerConfigData — it is a write-only field.
// The caller (PatchAdminConfig handler) is responsible for encrypting the value before
// calling UpsertServerConfigKey for the "oidc_client_secret" key.
type ServerConfigRepository interface {
	GetServerConfig(ctx context.Context) (*ServerConfigData, error)
	UpsertServerConfigKey(ctx context.Context, key, value string) error
}

// ServerConfigData holds the readable server configuration fields.
// oidc_client_secret is intentionally absent — it is a write-only field and
// must NEVER appear in any API response (AC#1 security invariant).
type ServerConfigData struct {
	InstanceName          string
	OIDCIssuer            string
	OIDCClientID          string
	AuditLogRetentionDays int
}

// dbServerConfigRepo is the real PostgreSQL implementation of ServerConfigRepository.
type dbServerConfigRepo struct {
	db *sql.DB
}

// NewServerConfigRepo constructs a new DB-backed ServerConfigRepository.
func NewServerConfigRepo(db *sql.DB) ServerConfigRepository {
	return &dbServerConfigRepo{db: db}
}

// GetServerConfig reads the readable server config keys from the server_config table.
// Missing keys return their documented defaults:
//   - instance_name      → ""
//   - oidc_issuer        → ""
//   - oidc_client_id     → ""
//   - audit_log_retention_days → 2555 (7 years)
//
// oidc_client_secret is intentionally NOT queried — it is a write-only field.
func (r *dbServerConfigRepo) GetServerConfig(ctx context.Context) (*ServerConfigData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM server_config
		 WHERE key IN ('instance_name', 'oidc_issuer', 'oidc_client_id', 'audit_log_retention_days')`)
	if err != nil {
		return nil, fmt.Errorf("GetServerConfig: %w", err)
	}
	defer rows.Close()

	vals := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("GetServerConfig: scan: %w", err)
		}
		vals[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetServerConfig: rows.Err: %w", err)
	}

	data := &ServerConfigData{
		InstanceName:          vals["instance_name"],
		OIDCIssuer:            vals["oidc_issuer"],
		OIDCClientID:          vals["oidc_client_id"],
		AuditLogRetentionDays: 2555, // default: 7 years
	}

	if retStr, ok := vals["audit_log_retention_days"]; ok && retStr != "" {
		var days int
		if _, err := fmt.Sscan(retStr, &days); err == nil && days >= 1 && days <= 36500 {
			data.AuditLogRetentionDays = days
		}
	}

	return data, nil
}

// UpsertServerConfigKey upserts a single key-value pair into server_config.
// Uses ON CONFLICT (key) DO UPDATE so that existing keys are updated.
// This matches the same pattern used by SaveAdminGroupClaim in gateway/internal/admin/auth.go.
func (r *dbServerConfigRepo) UpsertServerConfigKey(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
		key, value, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("UpsertServerConfigKey(%q): %w", key, err)
	}
	return nil
}
