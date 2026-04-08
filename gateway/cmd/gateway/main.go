package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nebu/nebu/internal/admin"
	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/buffer"
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

	// auth.NewProvider tolerates an unreachable OIDC provider at startup
	// (logs warning, starts background retry). LoginHandler checks Inner() != nil.
	oidcProvider := auth.NewProvider(ctx, cfg.OIDCIssuer)

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
	sessionGuard := admin.SessionGuard([]byte(internalSecret))

	// Legacy routes (backward compatibility — Story 3.10 will supersede)
	mux.HandleFunc("GET /admin/auth/login", adminAuth.LoginHandler)
	mux.HandleFunc("GET /admin/auth/callback", adminAuth.CallbackHandler)

	// New canonical routes (Story 3.9)
	mux.HandleFunc("GET /admin/login", adminAuth.LoginPageHandler)
	mux.HandleFunc("GET /admin/login/start", adminAuth.LoginStartHandler)
	mux.HandleFunc("GET /admin/callback", adminAuth.CallbackHandler)
	// Protected routes — require a valid admin session cookie (Story 3.11)
	mux.Handle("GET /admin/logout", sessionGuard(http.HandlerFunc(adminAuth.LogoutHandler)))

	// Dashboard route (Story 3.13) — registered BEFORE catch-all "GET /admin/"
	dashboardHandler := admin.NewDashboardHandler(tmplHandler, coreClient, bootstrapDB)
	mux.Handle("GET /admin/dashboard", sessionGuard(http.HandlerFunc(dashboardHandler.Handler)))

	checker := admin.NewPostgresBootstrapChecker(bootstrapDB)
	bootstrapHandler := admin.NewBootstrapHandler(checker, tmplHandler, bootstrapDB, []byte(internalSecret))
	guard := admin.BootstrapGuard(checker)

	// Static assets — no guard (needed to render bootstrap page)
	mux.HandleFunc("GET /admin/static/admin.css", admin.ServeCSS)
	mux.HandleFunc("GET /admin/static/fonts/{filename}", admin.ServeFontFile)
	mux.HandleFunc("GET /admin/static/vendor/{filename}", admin.ServeVendorFile)
	mux.HandleFunc("GET /admin/static/metrics-widget.js", admin.ServeMetricsWidgetJS)

	// SSE live metrics endpoint — behind session guard
	sseMetricsHandler := admin.NewSSEMetricsHandler(&coreMetricsAdapter{client: coreClient})
	mux.Handle("GET /admin/sse/metrics", sessionGuard(http.HandlerFunc(sseMetricsHandler.Handler)))

	// Bootstrap page — guarded (guard checks bootstrap state)
	mux.Handle("GET /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.Handler)))
	mux.Handle("POST /admin/bootstrap", guard(http.HandlerFunc(bootstrapHandler.StepHandler)))

	// Claim selection — shown after OIDC callback in bootstrap mode (no guard: bootstrap_completed not yet set)
	mux.HandleFunc("POST /admin/bootstrap/select-claim", adminAuth.ClaimSelectionHandler)

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

	loginHandler := matrix.NewLoginHandler(matrix.LoginConfig{
		DisplayName:   cfg.OIDCDisplayName,
		Provider:      oidcProvider,
		CoreClient:    coreClient,
		ServerName:    serverName,
		ClientID:      cfg.OIDCClientID,
		RoleClaimName: cfg.OIDCClaimRole,
	})
	mux.HandleFunc("GET /_matrix/client/v3/login", loginHandler.GetLogin)
	mux.HandleFunc("POST /_matrix/client/v3/login", loginHandler.PostLogin)

	tokenDB, err := sql.Open("pgx", cfg.DBURL)
	if err != nil {
		slog.Error("failed to open DB for token store", "err", err)
		os.Exit(1)
	}
	defer tokenDB.Close()
	tokenStore := db.NewPostgresTokenStore(tokenDB)
	logoutHandler := matrix.NewLogoutHandler(tokenStore)
	jwtMiddleware := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, tokenStore)
	mux.Handle("POST /_matrix/client/v3/logout", jwtMiddleware(http.HandlerFunc(logoutHandler.PostLogout)))

	createRoomHandler := matrix.NewCreateRoomHandler(matrix.CreateRoomConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/createRoom",
		jwtMiddleware(http.HandlerFunc(createRoomHandler.PostCreateRoom)))

	joinRoomHandler := matrix.NewJoinRoomHandler(matrix.JoinRoomConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	// FR20: Join by room ID or alias directly
	mux.Handle("POST /_matrix/client/v3/join/{roomIdOrAlias}",
		jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoom)))
	// Accept invitation via /rooms/{roomId}/join
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/join",
		jwtMiddleware(http.HandlerFunc(joinRoomHandler.PostJoinRoomById)))

	inviteHandler := matrix.NewInviteUserHandler(matrix.InviteUserConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/invite",
		jwtMiddleware(http.HandlerFunc(inviteHandler.PostInviteUser)))

	sendEventHandler := matrix.NewSendEventHandler(matrix.SendEventConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}",
		jwtMiddleware(http.HandlerFunc(sendEventHandler.PutSendEvent)))

	messagesHandler := matrix.NewGetMessagesHandler(matrix.GetMessagesConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/messages",
		jwtMiddleware(http.HandlerFunc(messagesHandler.GetMessages)))

	setRoomStateHandler := matrix.NewSetRoomStateHandler(matrix.SetRoomStateConfig{
		CoreClient: coreClient,
		ServerName: serverName,
	})
	// Register both: with stateKey (e.g. m.room.member/@user:srv) and without (e.g. m.room.power_levels).
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}",
		jwtMiddleware(http.HandlerFunc(setRoomStateHandler.PutSetRoomState)))
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}",
		jwtMiddleware(http.HandlerFunc(setRoomStateHandler.PutSetRoomState)))

	syncHandler := matrix.NewGetSyncHandler(matrix.GetSyncConfig{
		CoreClient: coreClient,
		ServerName: serverName,
		Buffer:     msgBuf,
	})
	mux.Handle("GET /_matrix/client/v3/sync",
		jwtMiddleware(http.HandlerFunc(syncHandler.GetSync)))

	slog.Info("HTTP server starting", "addr", ":8008")
	if err := http.ListenAndServe(":8008", mux); err != nil {
		slog.Error("HTTP server failed", "err", err)
		os.Exit(1)
	}
}
