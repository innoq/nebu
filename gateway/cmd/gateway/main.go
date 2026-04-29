package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nebu/nebu/internal/admin"
	"github.com/nebu/nebu/internal/audit"
	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/buffer"
	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/config"
	"github.com/nebu/nebu/internal/db"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/health"
	"github.com/nebu/nebu/internal/matrix"
	"github.com/nebu/nebu/internal/middleware"
	"github.com/nebu/nebu/internal/registry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// coreMetricsAdapter adapts *coregrpc.Client to satisfy the admin.MetricsReader interface.
type coreMetricsAdapter struct {
	client *coregrpc.Client
}

// coreRoomStateLookup adapts *coregrpc.Client to satisfy the buffer.RoomStateLookup
// interface required by buffer.RouteEventToUsers.
type coreRoomStateLookup struct {
	client *coregrpc.Client
}

// GetRoomState satisfies buffer.RoomStateLookup: calls the gRPC Core and returns member IDs.
func (a *coreRoomStateLookup) GetRoomState(ctx context.Context, roomID string) ([]string, error) {
	resp, err := a.client.GetRoomState(ctx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		return nil, err
	}
	return resp.GetMembers(), nil
}

// GetMetrics calls the gRPC stub and maps the response fields.
// If the stub returns nil response (Epic 4 not yet implemented), returns an error.
func (a *coreMetricsAdapter) GetMetrics(ctx context.Context) (float64, int, int, error) {
	resp, err := a.client.GetMetrics(ctx, &pb.GetMetricsRequest{})
	if err != nil || resp == nil {
		return 0, 0, 0, fmt.Errorf("core unavailable")
	}
	return float64(resp.MsgPerSec), int(resp.ActiveSessions), int(resp.RoomCount), nil
}

func main() {
	slog.Info("Nebu Gateway starting")

	// Main context: cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	// Warn when NEBU_TRUSTED_PROXY=true is combined with a non-HTTPS OIDC issuer or public
	// base URL. This combination almost always indicates a misconfiguration — the proxy
	// terminates TLS but the application URLs still reference plain HTTP.
	if os.Getenv("NEBU_TRUSTED_PROXY") == "true" {
		if strings.HasPrefix(cfg.OIDCIssuer, "http://") || strings.HasPrefix(os.Getenv("NEBU_PUBLIC_BASE_URL"), "http://") {
			slog.Warn("NEBU_TRUSTED_PROXY=true but OIDC issuer or public base URL uses http:// — likely misconfiguration")
		}
	}

	// auth.NewProvider tolerates an unreachable OIDC provider at startup
	// (logs warning, starts background retry). LoginHandler checks Inner() != nil.
	oidcProvider := auth.NewProvider(ctx, cfg.OIDCIssuer)

	if cfg.DBURL == "" {
		slog.Error("database configuration required", "error", "NEBU_DB_URL not set")
		os.Exit(1)
	}

	// Story 5.29a — AC3: migrations run as nebu_migrate (table owner).
	// NEBU_DB_URL_MIGRATE is the privileged role; falls back to NEBU_DB_URL if unset.
	if err := db.RunMigrations(cfg.DBURL, cfg.DBURLMigrate); err != nil {
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

	// Story 5.29a — AC10: gRPC client attaches PSK token to every call.
	// internalSecret is read later in this function; read it now for gRPC init.
	// Note: internalSecret is also used below for PSK middleware — reading once.
	pskBytesEarly, errEarlyPSK := os.ReadFile(cfg.InternalSecretFile)
	if errEarlyPSK != nil {
		slog.Error("failed to read internal secret file for gRPC auth", "path", cfg.InternalSecretFile, "err", errEarlyPSK)
		os.Exit(1)
	}
	internalSecretEarly := strings.TrimSpace(string(pskBytesEarly))

	coreClient, err := coregrpc.New(cfg.CoreGRPCAddr, internalSecretEarly)
	if err != nil {
		slog.Error("failed to create gRPC client", "err", err)
		os.Exit(1)
	}
	defer coreClient.Close()
	slog.Info("gRPC client initialized", "addr", cfg.CoreGRPCAddr)

	// Story 4-16: MessageBuffer — per-user in-memory ring buffer for event burst absorption.
	msgBuf := buffer.NewMessageBuffer(cfg.BufferCapacity, prometheus.DefaultRegisterer)

	// Start EventBus stream: one persistent gRPC server-streaming connection per gateway instance.
	eventStream := coregrpc.NewEventBusStream(coreClient.CoreServiceClient(), cfg.ServerName)
	eventStream.Start(ctx)

	// Event routing goroutine: reads EventBus events, routes to per-user ring buffers
	// based on room membership resolved via GetRoomState.
	roomLookup := &coreRoomStateLookup{client: coreClient}
	go func() {
		for event := range eventStream.Events() {
			buffer.RouteEventToUsers(ctx, event, msgBuf, roomLookup)
		}
	}()

	// Public HTTP server on :8080 (health, readiness, metrics — no auth)
	metrics := admin.NewMetrics(prometheus.DefaultRegisterer, coreClient)
	pubMux := http.NewServeMux()
	healthHandler := health.NewHandler(cfg.DBURL, coreClient)
	pubMux.HandleFunc("GET /health", healthHandler.Health)
	pubMux.HandleFunc("GET /ready", healthHandler.Ready)
	pubMux.Handle("GET /metrics", promhttp.Handler())

	go func() {
		handler := metrics.Middleware(pubMux)
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			srv := &http.Server{
				Addr:    ":8443",
				Handler: handler,
				TLSConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			}
			slog.Info("Public HTTPS server starting", "addr", ":8443")
			if err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
				slog.Error("Public HTTPS server failed", "err", err)
				os.Exit(1)
			}
		} else {
			if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
				slog.Error("Partial TLS configuration: both NEBU_TLS_CERT_FILE and NEBU_TLS_KEY_FILE must be set — falling back to plain HTTP")
			}
			slog.Warn("TLS disabled — not suitable for production")
			slog.Info("Public HTTP server starting", "addr", ":8080")
			if err := http.ListenAndServe(":8080", handler); err != nil {
				slog.Error("Public HTTP server failed", "err", err)
				os.Exit(1)
			}
		}
	}()

	// PSK was already read above (internalSecretEarly) for gRPC client init.
	// Reuse it here under the canonical name used by the rest of the function.
	internalSecret := internalSecretEarly

	// Set up HTTP mux with node registry behind PSK middleware
	mux := http.NewServeMux()

	// Story 5.20 — Body-limit middleware instances (declared early; used throughout).
	// bodyLimit1MiB: all Matrix JSON POST/PUT endpoints (AC 3).
	// bodyLimit64KiB: admin POST endpoints.
	bodyLimit1MiB := middleware.BodyLimitMiddleware(1 << 20)
	bodyLimit64KiB := middleware.BodyLimitMiddleware(64 * 1024)

	// Story 5.21 — Per-IP rate limiting tiers.
	// trustedProxy=true when the gateway sits behind a load-balancer that sets X-Forwarded-For.
	// SECURITY: the reverse proxy MUST strip any X-Forwarded-For header sent by external clients
	// so that only the proxy-appended IP (rightmost-minus-1) is trusted for rate limiting.
	trustedProxy := os.Getenv("NEBU_TRUSTED_PROXY") == "true"
	// strictRL: Matrix /login (real brute-force risk: username+password) — 30 req/min, burst 10.
	// (Compliance endpoints also use strictRL but are JWT-gated, so brute-force isn't a concern there.)
	strictRL := middleware.NewIPRateLimiter(middleware.RateLimitConfig{Rate: rate.Limit(30.0 / 60.0), Burst: 10}, trustedProxy, "strict")
	// adminRL: all rate-limited admin endpoints (login/start, callback, bootstrap, claim-select) —
	// 60 req/min, burst 20. Sized so legit admin clicking never hits the limit; sustained
	// hammering is still capped to ~1/sec which kills brute-force.
	adminRL := middleware.NewIPRateLimiter(middleware.RateLimitConfig{Rate: rate.Limit(60.0 / 60.0), Burst: 20}, trustedProxy, "admin")
	// mediumRL: SSO redirect/callback + public profile — 30 req/min, burst 10.
	mediumRL := middleware.NewIPRateLimiter(middleware.RateLimitConfig{Rate: rate.Limit(30.0 / 60.0), Burst: 10}, trustedProxy, "medium")
	// looseRL: remaining unauthenticated public endpoints — 300 req/min, burst 100.
	looseRL := middleware.NewIPRateLimiter(middleware.RateLimitConfig{Rate: rate.Limit(300.0 / 60.0), Burst: 100}, trustedProxy, "loose")
	reg := registry.New()
	regHandler := registry.NewHandler(reg)
	pskHandler := middleware.PSKMiddleware(internalSecret)(regHandler)

	mux.Handle("POST /internal/nodes/register", pskHandler)
	mux.Handle("GET /internal/nodes", pskHandler)

	bootstrapDB, err := sql.Open("pgx", cfg.DBURL)
	if err != nil {
		slog.Error("failed to open DB for bootstrap checker", "err", err)
		os.Exit(1)
	}
	defer bootstrapDB.Close()
	tmplHandler, err := admin.NewTemplateHandler()
	if err != nil {
		slog.Error("failed to initialize template handler", "err", err)
		os.Exit(1)
	}

	adminAuth := admin.NewAdminAuth(oidcProvider, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCClaimRole, []byte(internalSecret), bootstrapDB, tmplHandler)
	sessionStore := db.NewPostgresAdminSessionStore(bootstrapDB)
	adminAuth.SetSessionStore(sessionStore)
	adminAuth.SetCoreClient(coreClient.CoreServiceClient())
	sessionGuard := admin.SessionGuardWithStore([]byte(internalSecret), sessionStore)

	// AC5: Periodically clean up expired admin sessions (once per hour).
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := sessionStore.CleanupExpired(context.Background()); err != nil {
					slog.Warn("admin session cleanup failed", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Story 5.29c AC5 (FB-E5-07): Audit log retention purge scheduler — runs every 24h.
	// Reads retention_days from server_config (default 2555 = 7 years).
	// Uses a goroutine-based ticker so no external cron/queue is needed.
	go func() {
		retentionDays := loadAuditRetentionDays(bootstrapDB)
		auditDB, err := sql.Open("pgx", cfg.DBURL)
		if err != nil {
			slog.Error("audit scheduler: failed to open DB", "err", err)
			return
		}
		defer auditDB.Close()
		cleanupFn := func(ctx context.Context) (int64, error) {
			return audit.RunCleanup(ctx, auditDB, retentionDays)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		scheduler := audit.NewPurgeScheduler(retentionDays, cleanupFn, ticker.C)
		scheduler.Start(ctx)
	}()

	// Legacy routes (backward compatibility — Story 3.10 will supersede)
	mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)
	mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)

	// Story 5.13: CSRF double-submit-cookie middleware for all admin POST endpoints.
	csrf := admin.CSRFMiddleware()

	// New canonical routes (Story 3.9)
	// strictRL wraps login/start/callback — these are unauthenticated endpoints that trigger
	// OIDC flows and must be protected against brute-force / amplification attacks (Story 5.21).
	mux.Handle("GET /admin/login", adminRL(http.HandlerFunc(adminAuth.LoginPageHandler)))
	mux.Handle("GET /admin/login/start", adminRL(http.HandlerFunc(adminAuth.LoginStartHandler)))
	// /admin/callback: CSRF middleware runs first to rotate the token after login (AC6).
	mux.Handle("GET /admin/callback", adminRL(csrf(http.HandlerFunc(adminAuth.CallbackHandler))))
	// Protected routes — require a valid admin session cookie (Story 3.11)
	// GET /admin/logout intentionally returns 405 to prevent CSRF-logout via <img src="/admin/logout">.
	// All templates use a POST form (base.html). (MINOR-1 fix, Story 5.13)
	mux.HandleFunc("GET /admin/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed — use POST form to logout", http.StatusMethodNotAllowed)
	})
	// POST /admin/logout: CSRF-protected logout via form submission (Story 5.13, AC3).
	mux.Handle("POST /admin/logout", bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler)))))

	// Dashboard route (Story 3.13) — registered BEFORE catch-all "GET /admin/"
	dashboardHandler := admin.NewDashboardHandler(tmplHandler, coreClient, bootstrapDB)
	mux.Handle("GET /admin/dashboard", csrf(sessionGuard(http.HandlerFunc(dashboardHandler.Handler))))

	// Story 7.2: Users + Rooms master-detail routes — registered BEFORE catch-all
	usersHandler := admin.NewUsersHandler(tmplHandler)
	mux.Handle("GET /admin/users", csrf(sessionGuard(http.HandlerFunc(usersHandler.ListHandler))))
	mux.Handle("GET /admin/users/{userId}", csrf(sessionGuard(http.HandlerFunc(usersHandler.DetailHandler))))
	// Story 7.6: Display name update — intentionally NO csrf() wrapper (stub phase; see TODO in handler).
	// sessionGuard still applies — the admin must be logged in.
	mux.Handle("POST /admin/users/{userId}/display-name", sessionGuard(http.HandlerFunc(usersHandler.UpdateDisplayNameHandler)))
	// Story 7.7: Role update and deactivation — no csrf() wrapper (stub phase, same pattern as display-name).
	mux.Handle("POST /admin/users/{userId}/role", sessionGuard(http.HandlerFunc(usersHandler.UpdateRoleHandler)))
	mux.Handle("POST /admin/users/{userId}/deactivate", sessionGuard(http.HandlerFunc(usersHandler.DeactivateUserHandler)))
	roomsHandler := admin.NewRoomsHandler(tmplHandler)
	mux.Handle("GET /admin/rooms", csrf(sessionGuard(http.HandlerFunc(roomsHandler.ListHandler))))
	mux.Handle("GET /admin/rooms/{roomId}", csrf(sessionGuard(http.HandlerFunc(roomsHandler.DetailHandler))))
	// Story 7.9: Room name update and archive — no csrf() wrapper (stub phase; see TODO in handler).
	mux.Handle("POST /admin/rooms/{roomId}/name", sessionGuard(http.HandlerFunc(roomsHandler.UpdateRoomNameHandler)))
	mux.Handle("POST /admin/rooms/{roomId}/archive", sessionGuard(http.HandlerFunc(roomsHandler.ArchiveRoomHandler)))
	// Story 7.10: Server Configuration page.
	configHandler := admin.NewConfigHandler(tmplHandler)
	mux.Handle("GET /admin/config", csrf(sessionGuard(http.HandlerFunc(configHandler.Handler))))
	// POST /admin/config — no csrf() wrapper (stub phase; see TODO in handler); sessionGuard still applies.
	mux.Handle("POST /admin/config", sessionGuard(http.HandlerFunc(configHandler.UpdateConfigHandler)))

	checker := admin.NewPostgresBootstrapChecker(bootstrapDB)
	bootstrapHandler := admin.NewBootstrapHandler(checker, tmplHandler, bootstrapDB, []byte(internalSecret))
	guard := admin.BootstrapGuard(checker)

	// Static assets — no guard (needed to render bootstrap page)
	mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)
	mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)
	mux.HandleFunc("GET /admin/static/vendor/{filename}", admin.ServeVendorFile)
	mux.HandleFunc("GET /admin/static/metrics-widget.js", admin.ServeMetricsWidgetJS)
	mux.HandleFunc("GET /admin/static/js/{filename}", admin.ServeJSFile)

	// SSE live metrics endpoint — behind session guard (AC5: no CSRF on SSE/GET).
	sseMetricsHandler := admin.NewSSEMetricsHandler(&coreMetricsAdapter{client: coreClient})
	mux.Handle("GET /admin/sse/metrics", sessionGuard(http.HandlerFunc(sseMetricsHandler.Handler)))

	// Bootstrap page — CSRF middleware issues cookie on GET; verifies token on POST.
	// adminRL (60/min, burst 20) accommodates the multi-step wizard with comfortable
	// headroom; legitimate admin clicking should never trip the limiter.
	mux.Handle("GET /admin/bootstrap", adminRL(csrf(guard(http.HandlerFunc(bootstrapHandler.Handler)))))
	mux.Handle("POST /admin/bootstrap", adminRL(bodyLimit64KiB(csrf(guard(http.HandlerFunc(bootstrapHandler.StepHandler))))))

	// Claim selection — CSRF-protected (Story 5.13, AC3); also behind BootstrapGuard.
	mux.Handle("POST /admin/bootstrap/select-claim", adminRL(bodyLimit64KiB(csrf(guard(http.HandlerFunc(adminAuth.ClaimSelectionHandler))))))

	// Catch-all for unmatched /admin/* paths — redirect to bootstrap wizard if not yet set up,
	// otherwise show 404 (Go 1.22+ mux: most specific route wins, so this only fires for unknown paths).
	mux.HandleFunc("GET /admin/", func(w http.ResponseWriter, r *http.Request) {
		active, err := checker.IsBootstrapActive(r.Context())
		if err != nil {
			slog.Error("admin catch-all: bootstrap status check failed", "err", err)
			admin.Error500(w, r, tmplHandler)
			return
		}
		if active {
			http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
			return
		}
		// Bootstrap complete and path is exactly /admin or /admin/ — send to dashboard.
		// SessionGuard on /admin/dashboard will redirect to /admin/login if not authenticated.
		if r.URL.Path == "/admin" || r.URL.Path == "/admin/" {
			http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
			return
		}
		admin.Error404(w, r, tmplHandler)
	})

	// Matrix client discovery endpoints — required by all Matrix clients before login.
	// /_matrix/client/versions: signals Matrix protocol compatibility (FluffyChat, Element, etc. check this first).
	// /.well-known/matrix/client: auto-discovery of homeserver base URL.
	// looseRL: unauthenticated discovery endpoints (300 req/min, burst 100).
	mux.Handle("GET /_matrix/client/versions", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"versions":["v1.1","v1.2","v1.3","v1.4","v1.5","v1.6","v1.7","v1.8","v1.9","v1.10","v1.11"]}`))
	})))

	mux.Handle("GET /.well-known/matrix/client", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL := scheme + "://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, `{"m.homeserver":{"base_url":%q}}`, baseURL)
	})))

	loginHandler := matrix.NewLoginHandler(matrix.LoginConfig{
		DisplayName:        cfg.OIDCDisplayName,
		Provider:           oidcProvider,
		CoreClient:         coreClient,
		ServerName:         serverName,
		ClientID:           cfg.OIDCClientID,
		ClientSecret:       cfg.OIDCClientSecret,
		RoleClaimName:      cfg.OIDCClaimRole,
		SSORedirectSchemes: cfg.SSORedirectSchemes,
	})
	mux.Handle("GET /_matrix/client/v3/login", looseRL(http.HandlerFunc(loginHandler.GetLogin)))
	mux.Handle("POST /_matrix/client/v3/login", strictRL(bodyLimit1MiB(http.HandlerFunc(loginHandler.PostLogin))))

	// Matrix SSO: initiate PKCE flow, then callback from Dex.
	// /_matrix/client/v3/login/sso/redirect/oidc is registered in Dex redirectURIs.
	mux.Handle("GET /_matrix/client/v3/login/sso/redirect", mediumRL(http.HandlerFunc(loginHandler.GetSSORedirect)))
	mux.Handle("GET /_matrix/client/v3/login/sso/redirect/oidc", mediumRL(http.HandlerFunc(loginHandler.GetSSOCallback)))

	tokenDB, err := sql.Open("pgx", cfg.DBURL)
	if err != nil {
		slog.Error("failed to open DB for token store", "err", err)
		os.Exit(1)
	}
	defer tokenDB.Close()
	tokenStore := db.NewPostgresTokenStore(tokenDB)
	logoutHandler := matrix.NewLogoutHandler(tokenStore)
	jwtMiddleware := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, tokenStore, serverName)

	// Matrix compatibility endpoints — required by all Matrix clients post-login.
	// whoami: FluffyChat calls this immediately after login to verify the session is valid.
	mux.Handle("GET /_matrix/client/v3/account/whoami",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"user_id":%q,"is_guest":false}`, userID)
		})))

	// capabilities: Matrix clients check server feature flags before making API calls.
	// looseRL: unauthenticated capabilities endpoint (300 req/min, burst 100).
	mux.Handle("GET /_matrix/client/v3/capabilities", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"capabilities":{"m.change_password":{"enabled":false},"m.room_versions":{"default":"6","available":{"6":"stable"}}}}`))
	})))

	// MSC2965 OIDC-native auth metadata — not supported; return explicit 404 so
	// Element Web falls back to the standard m.login.sso flow instead of caching
	// a silent failure and breaking subsequent login attempts in non-private windows.
	// looseRL: unauthenticated metadata endpoint.
	mux.Handle("GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errcode":"M_UNRECOGNIZED","error":"MSC2965 OIDC-native auth is not supported by this server. Use m.login.sso."}`))
	})))

	// pushrules: return empty rule set — no push notifications in MVP.
	mux.Handle("GET /_matrix/client/v3/pushrules/",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"global":{"content":[],"override":[],"room":[],"sender":[],"underride":[]}}`))
		})))

	// media config: report the upload size limit (10 MiB default).
	// looseRL: unauthenticated media config endpoint.
	mux.Handle("GET /_matrix/media/v3/config", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"m.upload.size":10485760}`))
	})))

	// Stubs for endpoints FluffyChat requires but Nebu MVP does not yet implement.
	// All return valid empty responses so the client can proceed without errors.

	// 3PIDs (email/phone binding) — not supported in MVP.
	mux.Handle("GET /_matrix/client/v3/account/3pid",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"threepids":[]}`))
		})))

	// Device list — no multi-device management in MVP.
	mux.Handle("GET /_matrix/client/v3/devices",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"devices":[]}`))
		})))

	// Joined rooms — clients use this as a shortcut; sync already returns room state.
	mux.Handle("GET /_matrix/client/v3/joined_rooms",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"joined_rooms":[]}`))
		})))

	// User directory search — returns registered users matching the search term.
	// Queries the users table so Element Web can find other users by username.
	// Story 5-26: input validation, LIKE-metachar escaping, panic-fix, result cap.
	userDirHandler := matrix.NewUserDirectoryHandler(matrix.UserDirectoryConfig{
		DB:         db.NewPostgresUserDirectoryDB(bootstrapDB),
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/user_directory/search",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(userDirHandler.Search))))

	// Room directory / alias endpoints.
	// PUT: Element Web calls this when creating a public room with an address.
	// MVP: accept and acknowledge without storing — aliases not implemented yet.
	mux.Handle("PUT /_matrix/client/v3/directory/room/{roomAlias}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		}))))
	mux.Handle("DELETE /_matrix/client/v3/directory/room/{roomAlias}",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		})))
	mux.Handle("GET /_matrix/client/v3/directory/room/{roomAlias}", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"Room alias not found"}`))
	})))

	// Third-party protocol bridges — none in MVP.
	// looseRL: unauthenticated discovery endpoint.
	mux.Handle("GET /_matrix/client/v3/thirdparty/protocols", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})))

	// Event filter — clients POST a filter definition, receive a filter_id for use in /sync.
	// MVP: accept any filter and return id "0" (unfiltered sync is equivalent).
	mux.Handle("POST /_matrix/client/v3/user/{userId}/filter",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"filter_id":"0"}`))
		}))))

	// GET filter — Element Web fetches the stored filter on reconnect.
	// Without this endpoint, /sync enters a permanent ERROR loop (filter fetch fails → no sync).
	filterHandler := matrix.NewFilterHandler(matrix.FilterConfig{ServerName: serverName})
	mux.Handle("GET /_matrix/client/v3/user/{userId}/filter/{filterId}",
		jwtMiddleware(http.HandlerFunc(filterHandler.GetFilter)))

	// E2E encryption stubs — acknowledge without storing (no E2E in MVP).
	// Return non-zero one_time_key_counts so Element Web considers keys uploaded
	// and skips the "Setting up keys / Unable to set up keys" cross-signing dialog.
	e2eHandler := jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"one_time_key_counts":{"curve25519":50,"signed_curve25519":50}}`))
	}))
	mux.Handle("POST /_matrix/client/v3/keys/upload", bodyLimit1MiB(e2eHandler))
	mux.Handle("POST /_matrix/client/r0/keys/upload", bodyLimit1MiB(e2eHandler))

	// Cross-signing upload with UIA (User-Interactive Auth).
	// Element Web calls this to set up cross-signing keys.  The Matrix spec
	// requires a UIA challenge even though we don't actually enforce auth.
	// We implement the minimal UIA flow: m.login.dummy (no real challenge) so
	// Element considers the setup successful and skips the error dialog.
	mux.Handle("POST /_matrix/client/v3/keys/device_signing/upload",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			// If the request includes an "auth" field, treat it as the confirmed UIA step.
			if _, hasAuth := body["auth"]; hasAuth {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`))
				return
			}
			// First call (no auth): return UIA challenge with m.login.dummy.
			// m.login.dummy requires no actual credentials — Element completes it
			// automatically, allowing the cross-signing setup to succeed silently.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"flows":[{"stages":["m.login.dummy"]}],"params":{},"session":"%s"}`,
				fmt.Sprintf("%x", func() []byte { b := make([]byte, 8); _, _ = rand.Read(b); return b }()))
		}))))
	mux.Handle("POST /_matrix/client/v3/keys/signatures/upload",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"failures":{}}`))
		}))))

	// Key backup stubs — Element Web tries to create/fetch key backups.
	// Returning 404 for GET (no backup) and 200 for POST (accept creation silently)
	// prevents the "Unable to set up keys" error dialog from appearing.
	mux.Handle("GET /_matrix/client/v3/room_keys/version",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"No backup found"}`))
		})))
	mux.Handle("POST /_matrix/client/v3/room_keys/version",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"1"}`))
		}))))

	// Account data endpoints (used for secret storage, notification settings, etc.)
	mux.Handle("GET /_matrix/client/v3/user/{userId}/account_data/{type}",
		jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"Account data not found"}`))
		})))
	mux.Handle("PUT /_matrix/client/v3/user/{userId}/account_data/{type}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		}))))

	// Misc stubs to suppress other 404s in Element Web startup
	// looseRL on unauthenticated stub endpoints that clients poll at startup.
	mux.Handle("GET /_matrix/client/v3/voip/turnServer", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"TURN not configured"}`))
	})))
	mux.Handle("GET /_matrix/client/unstable/org.matrix.msc3814.v1/dehydrated_device", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"Dehydrated device not supported"}`))
	})))
	mux.Handle("GET /_matrix/client/unstable/org.matrix.msc4143/rtc/transports", looseRL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ice_servers":[]}`))
	})))

	// Story 5-29e: improved keys/query stub — returns device_keys entry for known users.
	keysQueryHandler := matrix.NewKeysQueryHandler(matrix.KeysQueryConfig{
		UserChecker: db.NewPostgresUserExistenceChecker(bootstrapDB),
	})
	mux.Handle("POST /_matrix/client/v3/keys/query",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(keysQueryHandler.PostKeysQuery))))

	// keys/changes requires JWT auth per Matrix spec (AC7, story 5-27).
	mux.Handle("GET /_matrix/client/v3/keys/changes", looseRL(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"changed":[],"left":[]}`))
	}))))

	mux.Handle("POST /_matrix/client/v3/keys/claim",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"one_time_keys":{},"failures":{}}`))
		}))))

	mux.Handle("POST /_matrix/client/v3/logout", bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(logoutHandler.PostLogout))))

	createRoomHandler := matrix.NewCreateRoomHandler(matrix.CreateRoomConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/createRoom",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(createRoomHandler.PostCreateRoom))))

	joinRoomHandler := matrix.NewJoinRoomHandler(matrix.JoinRoomConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	// FR20: Join by room ID or alias directly
	mux.Handle("POST /_matrix/client/v3/join/{roomIdOrAlias}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoom))))
	// Accept invitation via /rooms/{roomId}/join
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/join",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoomById))))

	inviteHandler := matrix.NewInviteUserHandler(matrix.InviteUserConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/invite",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(inviteHandler.PostInviteUser))))

	// Story 5-29e Bug 1: upgrade endpoint was missing, returning 404 from default mux fallback.
	// This 501 stub prevents the 404 and signals the client that the server understands the
	// endpoint but does not yet implement full room upgrade (tombstone + new room + state copy).
	upgradeRoomHandler := matrix.NewUpgradeRoomHandler(matrix.UpgradeRoomConfig{
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/upgrade",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(upgradeRoomHandler.PostUpgradeRoom))))

	sendEventHandler := matrix.NewSendEventHandler(matrix.SendEventConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(sendEventHandler.PutSendEvent))))

	messagesHandler := matrix.NewGetMessagesHandler(matrix.GetMessagesConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	// GetRoomMessages wraps GetMessages with Matrix roomId path-param validation (AC2, story 5-27).
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/messages",
		jwtMiddleware(http.HandlerFunc(messagesHandler.GetRoomMessages)))

	setRoomStateHandler := matrix.NewSetRoomStateHandler(matrix.SetRoomStateConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	// Register both: with stateKey (e.g. m.room.member/@user:srv) and without (e.g. m.room.power_levels).
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(setRoomStateHandler.PutSetRoomState))))
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(setRoomStateHandler.PutSetRoomState))))

	syncHandler := matrix.NewGetSyncHandler(matrix.GetSyncConfig{
		CoreClient: coreClient,
		ServerName: serverName,
		Buffer:     msgBuf,
		DB:         bootstrapDB, // for rooms.invite pending invitation queries
	})
	mux.Handle("GET /_matrix/client/v3/sync",
		jwtMiddleware(http.HandlerFunc(syncHandler.GetSync)))

	typingHandler := matrix.NewTypingHandler(matrix.TypingConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(typingHandler.PutTyping))))

	receiptsHandler := matrix.NewReceiptsHandler(matrix.ReceiptsConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(receiptsHandler.PostReceipt))))

	// Room members — Element Web calls this to populate the member sidebar after entering a room.
	getRoomMembersHandler := matrix.NewGetRoomMembersHandler(matrix.GetRoomMembersConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/members",
		jwtMiddleware(http.HandlerFunc(getRoomMembersHandler.GetRoomMembers)))

	// Read markers — Element Web posts fully-read markers; acknowledge without persisting (MVP).
	// Without this, Element enters a retry loop producing "Error sending fully_read" log spam.
	readMarkersHandler := matrix.NewReadMarkersHandler(matrix.ReadMarkersConfig{ServerName: serverName})
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/read_markers",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(readMarkersHandler.PostReadMarkers))))

	// Profile DB: reuse the bootstrapDB connection for direct profile reads (GET /profile — no gRPC).
	profileHandler := matrix.NewProfileHandler(matrix.ProfileConfig{
		CoreClient: coreClient,
		ServerName: serverName,
		DB:         db.NewPostgresProfileDB(bootstrapDB),
	})
	// GET is unauthenticated — no jwtMiddleware wrapper (per Matrix spec: profile is public).
	// GET /profile is unauthenticated (Matrix spec: profile is public) — medium rate-limit (Story 5.21, AC 2).
	mux.Handle("GET /_matrix/client/v3/profile/{userId}", mediumRL(http.HandlerFunc(profileHandler.GetProfile)))
	// PUT endpoints require JWT auth.
	mux.Handle("PUT /_matrix/client/v3/profile/{userId}/displayname",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(profileHandler.PutDisplayname))))
	mux.Handle("PUT /_matrix/client/v3/profile/{userId}/avatar_url",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(profileHandler.PutAvatarURL))))

	presenceHandler := matrix.NewPresenceHandler(matrix.PresenceConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("GET /_matrix/client/v3/presence/{userId}/status",
		jwtMiddleware(http.HandlerFunc(presenceHandler.GetPresenceStatus)))

	// PUT /presence/{userId}/status — checks userId == authed user (AC5, story 5-27).
	mux.Handle("PUT /_matrix/client/v3/presence/{userId}/status",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(presenceHandler.PutPresenceStatus))))

	// Story 5.3 — Compliance Access Request API
	// Route namespace: /api/v1/compliance/* — NOT under /_matrix/client/v3/ (Matrix CS API)
	// and NOT under /admin/ (admin web UI). Same HTTP port (:8008), distinct path prefix.
	// bodyLimit64KiB: compliance request body is administrative data (not Matrix message payload).
	complianceDB, err := sql.Open("pgx", cfg.DBURL)
	if err != nil {
		slog.Error("failed to open DB for compliance handler", "err", err)
		os.Exit(1)
	}
	defer complianceDB.Close()
	accessRequestHandler := &compliance.AccessRequestHandler{
		DB:         complianceDB,
		CoreClient: coreClient.CoreServiceClient(),
	}
	// FB-53-01: all compliance/* and admin anonymize/key-delete routes wrapped in strictRL (10/min/IP).
	mux.Handle("POST /api/v1/compliance/access-requests",
		strictRL(bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostAccessRequest)))))

	// Story 5.4 — Four-Eyes Approval API
	// GET: no body, so no bodyLimit needed
	mux.Handle("GET /api/v1/compliance/access-requests",
		strictRL(jwtMiddleware(http.HandlerFunc(accessRequestHandler.GetAccessRequests))))

	mux.Handle("POST /api/v1/compliance/access-requests/{requestId}/approve",
		strictRL(bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostApprove)))))

	mux.Handle("POST /api/v1/compliance/access-requests/{requestId}/reject",
		strictRL(bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostReject)))))

	// Admin API (session auth, not JWT) — pending-count badge for dashboard
	pendingCountHandler := &compliance.PendingCountHandler{DB: complianceDB}
	mux.Handle("GET /admin/api/compliance/pending-count",
		sessionGuard(http.HandlerFunc(pendingCountHandler.Handler)))

	// Story 5.5 — Compliance Session Issuance
	// Seed / load the compliance signing Ed25519 keypair from server_config.
	// This key is persisted (unlike :nebu_signing_key in Elixir, which is ephemeral).
	// The key is read once at startup; it lives in process memory during runtime.
	//
	// Story 5.29c AC9: key is stored encrypted via AES-256-GCM.
	// NEBU_KEY_ENCRYPTION_KEY: 32-byte hex master key from env (or dev default).
	//
	// Story 5.29d AC5 (FB-29c-1): Hard-fail in production when KEK is missing,
	// unless NEBU_ALLOW_INSECURE_KEK=true is explicitly set.
	kekHex := os.Getenv("NEBU_KEY_ENCRYPTION_KEY")
	if err := validateKEKConfig(kekHex, cfg.Env, cfg.AllowInsecureKEK); err != nil {
		slog.Error("KEK configuration rejected: " + err.Error())
		os.Exit(1)
	}
	if kekHex == "" {
		// Dev-only default: all-zeros 32 bytes. NOT suitable for production.
		// validateKEKConfig already warned above; this branch is only reached in non-production.
		kekHex = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	kekBytes, err := hex.DecodeString(kekHex)
	if err != nil || len(kekBytes) != 32 {
		slog.Error("NEBU_KEY_ENCRYPTION_KEY must be 64 hex chars (32 bytes)", "err", err)
		os.Exit(1)
	}
	keyEncFn := newAES256GCMEncrypt(kekBytes)
	keyDecFn := newAES256GCMDecrypt(kekBytes)

	// One-time legacy migration: pre-5.29c deployments stored the compliance
	// signing key as plaintext hex in server_config. server_config has only
	// INSERT and SELECT policies under FORCE RLS so the runtime nebu_app role
	// cannot UPDATE it — we use the migration role (NEBU_DB_URL_MIGRATE,
	// nebu_migrate has BYPASSRLS) to rewrite the row to the new "enc:" format.
	// Idempotent: no-op for fresh deployments and for already-encrypted rows.
	if cfg.DBURLMigrate != "" {
		migrateDB, mErr := sql.Open("pgx", cfg.DBURLMigrate)
		if mErr != nil {
			slog.Error("failed to open migrate DB for compliance key migration", "err", mErr)
			os.Exit(1)
		}
		if mErr := compliance.MigrateLegacyPlaintextKey(ctx, migrateDB, keyEncFn); mErr != nil {
			slog.Error("MigrateLegacyPlaintextKey failed", "err", mErr)
			_ = migrateDB.Close()
			os.Exit(1)
		}
		_ = migrateDB.Close()
	}

	compSignKey, compPubKey, err := compliance.EnsureComplianceSigningKey(ctx, complianceDB, keyEncFn, keyDecFn)
	if err != nil {
		slog.Error("failed to seed/load compliance signing key", "err", err)
		os.Exit(1)
	}
	sessionHandler := &compliance.SessionHandler{
		DB:         complianceDB,
		CoreClient: coreClient.CoreServiceClient(),
		SigningKey: compSignKey,
		PublicKey:  compPubKey,
	}
	mux.Handle("POST /api/v1/compliance/access-requests/{requestId}/session",
		strictRL(bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(sessionHandler.PostSession)))))

	// Story 5.6 — Compliance Data Export
	// GET endpoint — no body, so no bodyLimit64KiB or requireJSON needed.
	// All export scope comes from the validated X-Compliance-Token claims (not URL params).
	exportHandler := &compliance.ExportHandler{
		DB:         complianceDB,
		CoreClient: coreClient.CoreServiceClient(),
		SigningKey: compSignKey,
		PublicKey:  compPubKey,
	}
	mux.Handle("GET /api/v1/compliance/export",
		strictRL(jwtMiddleware(http.HandlerFunc(exportHandler.GetExport))))

	// Story 5.29c AC2 — Compliance session revoke endpoint.
	// POST /api/v1/admin/compliance/sessions/{sessionId}/revoke
	// Auth: sessionGuard (admin session, not JWT) — analogous to pending-count (Story 5.4).
	// Role gate: instance_admin only (enforced inside handler).
	// CSRF: state-changing cookie-authenticated POST — must be wrapped in csrf
	// like every other admin POST (Logout, Bootstrap, Select-Claim). Without
	// this, a lure-attack would let an attacker revoke compliance sessions
	// via a forged form post. Kassandra HIGH-1 fix (2026-04-29).
	revokeSessionHandler := &compliance.RevokeSessionHandler{
		DB:         complianceDB,
		CoreClient: coreClient.CoreServiceClient(),
	}
	mux.Handle("POST /api/v1/admin/compliance/sessions/{sessionId}/revoke",
		adminRL(bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(revokeSessionHandler.RevokeSession))))))

	// Story 5.7 — DSGVO User Key Deletion
	// Route namespace: /api/v1/admin/* — instance_admin only, role gate inside handler.
	// bodyLimit64KiB: small deletion request body (reason string + userId path param).
	userKeyDeletionHandler := &compliance.UserKeyDeletionHandler{
		CoreClient: coreClient.CoreServiceClient(),
	}
	mux.Handle("DELETE /api/v1/admin/users/{userId}/keys",
		strictRL(bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(userKeyDeletionHandler.DeleteUserKeys)))))

	// Story 5.8 — Operational PII Anonymization
	// Route namespace: /api/v1/admin/* — instance_admin only, role gate inside handler.
	// No body expected (POST without payload), so no bodyLimit needed.
	// jwtMiddleware is required for authentication/role extraction.
	anonymizationHandler := &compliance.AnonymizationHandler{
		DB:          complianceDB,
		CoreClient:  coreClient.CoreServiceClient(),
		StoragePath: os.Getenv("NEBU_MEDIA_STORAGE_PATH"),
	}
	mux.Handle("POST /api/v1/admin/users/{userId}/anonymize",
		strictRL(jwtMiddleware(http.HandlerFunc(anonymizationHandler.AnonymizeUser))))

	// POST /rooms/{roomId}/leave — leave a room (calls Elixir LeaveRoom gRPC)
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/leave",
		bodyLimit1MiB(jwtMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roomID := r.PathValue("roomId")
			userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
			systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
			grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
			_, err := coreClient.LeaveRoom(grpcCtx, &pb.LeaveRoomRequest{
				RoomId: roomID,
				UserId: userID,
			})
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				st, _ := status.FromError(err)
				switch st.Code() {
				case codes.NotFound:
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprintf(w, `{"errcode":"M_NOT_FOUND","error":"Room not found"}`)
				case codes.PermissionDenied:
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprintf(w, `{"errcode":"M_FORBIDDEN","error":"Not a member of this room"}`)
				default:
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, `{"errcode":"M_UNKNOWN","error":"Internal server error"}`)
				}
				return
			}
			w.Write([]byte(`{}`))
		}))))

	// Story 5.14: Wrap the main mux so that every /admin/* response carries security headers.
	// SecurityHeadersMiddleware is the outermost layer — even 302 redirects emitted by
	// SessionGuard / BootstrapGuard will include the headers.
	//
	// CSP form-action allowlist must include the OIDC issuer origin so the bootstrap
	// step-2 form (POST /admin/bootstrap → 303 → 302 → OIDC provider) can redirect
	// cross-origin without being silently blocked.
	oidcIssuerOrigin := extractOriginOrEmpty(cfg.OIDCIssuer)
	adminHandler := admin.SecurityHeadersMiddleware(oidcIssuerOrigin)(mux)
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			adminHandler.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	// Story 5.20 — Explicit server timeouts to guard against Slowloris and idle-
	// connection exhaustion (AC 1).
	//
	// WriteTimeout is 60s rather than a lower value because /_matrix/client/v3/sync
	// uses long-polling (Matrix CS API spec §11.5): clients may hold the connection
	// open for up to 30 s waiting for new events.  60 s provides headroom for a
	// full-length poll plus response serialisation.
	srv := &http.Server{
		Addr:              ":8008",
		Handler:           mainHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	slog.Info("HTTP server starting", "addr", ":8008")
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}

// extractOriginOrEmpty returns the scheme://host[:port] of issuer or "" if
// issuer is empty / unparseable. Used to widen CSP form-action so the bootstrap
// step-2 form may redirect the browser to the OIDC provider without a silent
// CSP block. Trusted because NEBU_OIDC_ISSUER is operator-set and validated
// elsewhere via validate.IssuerURL.
func extractOriginOrEmpty(issuer string) string {
	if issuer == "" {
		return ""
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// loadAuditRetentionDays reads audit_log_retention_days from server_config.
// Falls back to 2555 (7 years) if the key is missing or unparseable.
func loadAuditRetentionDays(db *sql.DB) int {
	const defaultDays = 2555
	var val string
	err := db.QueryRowContext(context.Background(),
		`SELECT value FROM server_config WHERE key = 'audit_log_retention_days'`,
	).Scan(&val)
	if err != nil {
		return defaultDays
	}
	days, err := strconv.Atoi(val)
	if err != nil || days < 1 || days > 36500 {
		slog.Warn("audit: invalid audit_log_retention_days in server_config — using default",
			"raw_value", val, "default", defaultDays)
		return defaultDays
	}
	return days
}

// newAES256GCMEncrypt returns a KeyEncryptFn backed by AES-256-GCM with the given master key.
// The ciphertext format is: nonce (12 bytes) || ciphertext (plaintext + 16-byte tag).
func newAES256GCMEncrypt(masterKey []byte) compliance.KeyEncryptFn {
	return func(plaintext []byte) ([]byte, error) {
		block, err := aes.NewCipher(masterKey)
		if err != nil {
			return nil, fmt.Errorf("AES cipher: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("GCM: %w", err)
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("nonce: %w", err)
		}
		ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
		return ciphertext, nil
	}
}

// newAES256GCMDecrypt returns a KeyDecryptFn backed by AES-256-GCM with the given master key.
func newAES256GCMDecrypt(masterKey []byte) compliance.KeyDecryptFn {
	return func(ciphertext []byte) ([]byte, error) {
		block, err := aes.NewCipher(masterKey)
		if err != nil {
			return nil, fmt.Errorf("AES cipher: %w", err)
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("GCM: %w", err)
		}
		if len(ciphertext) < gcm.NonceSize() {
			return nil, fmt.Errorf("ciphertext too short")
		}
		nonce, ciphertextBody := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, ciphertextBody, nil)
		if err != nil {
			return nil, fmt.Errorf("GCM decrypt: %w", err)
		}
		return plaintext, nil
	}
}

// ensureComplianceSigningKey reads the compliance Ed25519 keypair from server_config.
// If the rows do not exist, a new keypair is generated and persisted.
//
// Keys are stored as hex-encoded TEXT in server_config:
//
//	key='compliance_signing_key_priv' — hex(64-byte Ed25519 private key seed||public)
//	key='compliance_signing_key_pub'  — hex(32-byte Ed25519 public key)
//
// This key is separate from :nebu_signing_key (ephemeral in Elixir — regenerated on
// every Application.start/2). Compliance JWTs must survive an Elixir restart.
func ensureComplianceSigningKey(db *sql.DB) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	ctx := context.Background()

	// Try to load existing keys.
	rows, err := db.QueryContext(ctx,
		`SELECT key, value FROM server_config
		  WHERE key IN ('compliance_signing_key_priv', 'compliance_signing_key_pub')`)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: query server_config: %w", err)
	}
	defer rows.Close()

	kvs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, nil, fmt.Errorf("ensureComplianceSigningKey: scan row: %w", err)
		}
		kvs[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: rows.Err: %w", err)
	}

	privHex, hasPriv := kvs["compliance_signing_key_priv"]
	pubHex, hasPub := kvs["compliance_signing_key_pub"]

	if hasPriv && hasPub {
		// Parse existing keys from hex.
		privBytes, err := hex.DecodeString(privHex)
		if err != nil {
			return nil, nil, fmt.Errorf("ensureComplianceSigningKey: decode priv hex: %w", err)
		}
		pubBytes, err := hex.DecodeString(pubHex)
		if err != nil {
			return nil, nil, fmt.Errorf("ensureComplianceSigningKey: decode pub hex: %w", err)
		}
		return ed25519.PrivateKey(privBytes), ed25519.PublicKey(pubBytes), nil
	}

	// Generate a new Ed25519 keypair.
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: generate key: %w", err)
	}

	privHex = hex.EncodeToString(privKey)
	pubHex = hex.EncodeToString(pubKey)

	// INSERT both keys with ON CONFLICT DO NOTHING. If another gateway instance
	// raced us and already inserted, our generated key is discarded — we re-read
	// and use the persisted key. This preserves any tokens already signed with
	// the persisted key. (Using DO UPDATE SET value=EXCLUDED.value would silently
	// invalidate every token issued by the first writer.)
	// Kassandra MEDIUM-3 (2026-04-23): server_config.set_at is BIGINT NOT NULL
	// without a default — first cold-start crashed before this fix.
	setAt := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES
		   ('compliance_signing_key_priv', $1, $3),
		   ('compliance_signing_key_pub',  $2, $3)
		 ON CONFLICT (key) DO NOTHING`,
		privHex, pubHex, setAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: insert keys: %w", err)
	}

	// Re-read to obtain whichever pair actually won the race (ours or another
	// instance's). The pair is guaranteed to be present after the INSERT above.
	persistedRows, err := db.QueryContext(ctx,
		`SELECT key, value FROM server_config
		  WHERE key IN ('compliance_signing_key_priv', 'compliance_signing_key_pub')`)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: re-read after insert: %w", err)
	}
	defer persistedRows.Close()

	persisted := make(map[string]string)
	for persistedRows.Next() {
		var k, v string
		if err := persistedRows.Scan(&k, &v); err != nil {
			return nil, nil, fmt.Errorf("ensureComplianceSigningKey: re-read scan: %w", err)
		}
		persisted[k] = v
	}
	if err := persistedRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: re-read rows.Err: %w", err)
	}

	persistedPriv, okPriv := persisted["compliance_signing_key_priv"]
	persistedPub, okPub := persisted["compliance_signing_key_pub"]
	if !okPriv || !okPub {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: keys missing after insert (got %d rows)", len(persisted))
	}

	privBytes, err := hex.DecodeString(persistedPriv)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: decode persisted priv hex: %w", err)
	}
	pubBytes, err := hex.DecodeString(persistedPub)
	if err != nil {
		return nil, nil, fmt.Errorf("ensureComplianceSigningKey: decode persisted pub hex: %w", err)
	}

	if persistedPriv == privHex {
		slog.Info("compliance signing key generated and stored in server_config")
	} else {
		slog.Info("compliance signing key already present (won by concurrent instance) — using persisted key")
	}
	return ed25519.PrivateKey(privBytes), ed25519.PublicKey(pubBytes), nil
}
