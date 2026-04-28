//go:build integration

package integration_test

// Story 5.29a — Block B: Authenticated gRPC channel (FB-52-01)
//
// These tests verify that the internal gRPC channel between gateway and core
// requires authentication (shared-secret PSK interceptor or mTLS) and that
// unauthenticated/forged connections are rejected.
//
// ALL tests FAIL until:
//   - Elixir core implements a gRPC server interceptor that checks metadata token
//   - Go gateway attaches the PSK token from .secrets/internal_secret in all gRPC calls
//   - The interceptor rejects requests without a valid token with codes.Unauthenticated
//
// Failing reasons (before implementation):
//   - Core currently accepts all gRPC calls without authentication (insecure.NewCredentials)
//   - No token check in Elixir interceptor
//
// Build tag: integration — run with:
//   go test -tags=integration ./test/integration/... -v -run TestCoreGRPC
//
// Environment variables:
//   NEBU_TEST_CORE_GRPC_ADDR      — gRPC address of core (default: core:9000)
//   NEBU_TEST_INTERNAL_SECRET     — PSK token read from .secrets/internal_secret
//   NEBU_TEST_DB_URL              — DB connection for row-count verification in Test 10

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// coreGRPCAddr returns the gRPC address for the core service from env.
func coreGRPCAddr() string {
	addr := os.Getenv("NEBU_TEST_CORE_GRPC_ADDR")
	if addr == "" {
		return "core:9000"
	}
	return addr
}

// dialUnauthenticatedCore dials the core gRPC server with insecure credentials
// and NO auth metadata — simulating an attacker with L4 access.
func dialUnauthenticatedCore(t *testing.T) pb.CoreServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(
		coreGRPCAddr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient (unauthenticated): %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCoreServiceClient(conn)
}

// dialWithToken dials the core gRPC server and attaches a fixed token to every call
// via a UnaryClientInterceptor.
func dialWithToken(t *testing.T, token string) pb.CoreServiceClient {
	t.Helper()
	// perRPCCredentials embeds the token into outgoing metadata.
	interceptor := grpc.WithUnaryInterceptor(func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		md := metadata.Pairs("x-nebu-node-token", token)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return invoker(ctx, method, req, reply, cc, opts...)
	})

	conn, err := grpc.NewClient(
		coreGRPCAddr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		interceptor,
	)
	if err != nil {
		t.Fatalf("grpc.NewClient (with token): %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCoreServiceClient(conn)
}

// validPSK reads the PSK from NEBU_TEST_INTERNAL_SECRET env var.
func validPSK(t *testing.T) string {
	t.Helper()
	psk := os.Getenv("NEBU_TEST_INTERNAL_SECRET")
	if psk == "" {
		t.Skip("NEBU_TEST_INTERNAL_SECRET not set — skipping gRPC auth test")
	}
	return psk
}

// TestCoreGRPC_RejectsUnauthenticatedDial — AC7/AC9
//
// Given: Core gRPC server with auth interceptor active
// When:  Raw grpc.Dial with insecure credentials and no auth metadata
//        sends a WriteAuditLog RPC
// Then:  The core returns codes.Unauthenticated
//
// FAILS until the Elixir gRPC interceptor rejects requests without a valid token.
func TestCoreGRPC_RejectsUnauthenticatedDial(t *testing.T) {
	client := dialUnauthenticatedCore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteAuditLog(ctx, &pb.WriteAuditLogRequest{
		ActorUserId: "attacker",
		Action:      "forge_attempt_unauthenticated",
		Outcome:     "success",
	})

	if err == nil {
		t.Fatal("AC9 FAIL: WriteAuditLog succeeded without authentication — " +
			"core gRPC server must reject unauthenticated calls with codes.Unauthenticated. " +
			"Implement a gRPC server interceptor in the Elixir core that checks " +
			"x-nebu-node-token metadata against the PSK.")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("AC9 FAIL: error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("AC9 FAIL: expected codes.Unauthenticated, got %v (%v)", st.Code(), st.Message())
	} else {
		t.Logf("AC9 PASS: unauthenticated call correctly rejected: %v", st.Code())
	}
}

// TestCoreGRPC_RejectsForgedToken — AC7/AC9
//
// Given: Core gRPC server with auth interceptor active
// When:  A client sends a randomly forged (non-PSK) token in x-nebu-node-token metadata
// Then:  The core returns codes.Unauthenticated
//
// FAILS until the Elixir gRPC interceptor validates the token against the PSK.
func TestCoreGRPC_RejectsForgedToken(t *testing.T) {
	// Use a clearly invalid token.
	forgedToken := "forged-token-this-is-not-the-psk-0000000000000000"
	client := dialWithToken(t, forgedToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteAuditLog(ctx, &pb.WriteAuditLogRequest{
		ActorUserId: "attacker",
		Action:      "forge_attempt_bad_token",
		Outcome:     "success",
	})

	if err == nil {
		t.Fatal("AC9 FAIL: WriteAuditLog succeeded with a forged token — " +
			"the PSK check must validate the exact token value and reject anything else.")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("AC9 FAIL: error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("AC9 FAIL: expected codes.Unauthenticated for forged token, got %v (%v)",
			st.Code(), st.Message())
	} else {
		t.Logf("AC9 PASS: forged token correctly rejected: %v", st.Code())
	}
}

// TestCoreGRPC_AcceptsValidToken — AC7/AC10
//
// Given: Core gRPC server with auth interceptor active
// When:  A client sends the valid PSK from .secrets/internal_secret in metadata
// Then:  The RPC call succeeds (returns OK or a domain-level error, not Unauthenticated)
//
// FAILS until Go gateway and Elixir core both use the PSK for transport auth.
func TestCoreGRPC_AcceptsValidToken(t *testing.T) {
	psk := validPSK(t)
	client := dialWithToken(t, psk)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.WriteAuditLog(ctx, &pb.WriteAuditLogRequest{
		ActorUserId: "sys-grpc-auth-test",
		Action:      "grpc_auth_valid_token_test",
		Outcome:     "success",
	})

	if err != nil {
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("AC10 FAIL: error is not a gRPC status: %v", err)
		}
		switch st.Code() {
		case codes.Unauthenticated:
			t.Fatalf("AC10 FAIL: valid PSK token was rejected with Unauthenticated — "+
				"check that the Elixir interceptor reads the token from the same key "+
				"as the Go gateway writes it (x-nebu-node-token). Error: %v", err)
		case codes.OK,
			codes.Unimplemented,
			codes.NotFound,
			codes.InvalidArgument,
			codes.FailedPrecondition,
			codes.AlreadyExists,
			codes.PermissionDenied:
			// Transport auth passed; a domain-level error is acceptable for this AC.
			t.Logf("AC10 NOTE: RPC returned non-Unauthenticated domain error (transport auth passed): %v", err)
			return
		default:
			// codes.Internal / codes.Unknown / codes.Unavailable etc. would hide
			// real Elixir crashes — fail loudly so we never silently mask them.
			t.Fatalf("AC10 FAIL: RPC returned %v which would mask server crashes — "+
				"AC10 only accepts Unimplemented/NotFound/InvalidArgument/FailedPrecondition/"+
				"AlreadyExists/PermissionDenied as transport-auth-passed evidence. Error: %v",
				st.Code(), err)
		}
	}
	t.Log("AC10 PASS: WriteAuditLog succeeded with valid PSK token")
}

// TestAuditForgery_NoRowInserted — AC7/AC11
//
// End-to-end forgery test:
//   Given: A live DB and a live core gRPC server with auth interceptor active
//   When:  An unauthenticated WriteAuditLog RPC is attempted (no token in metadata)
//   Then:  The RPC returns Unauthenticated AND the audit_log row count is unchanged
//
// This test combines the gRPC rejection with a DB verification step to prove
// that the interceptor fires BEFORE any state mutation occurs.
//
// FAILS until:
//   1. The gRPC interceptor rejects unauthenticated calls
//   2. The DB audit_log table stays consistent (no partial insert)
func TestAuditForgery_NoRowInserted(t *testing.T) {
	// Step 1: Read the baseline row count.
	dsn := os.Getenv("NEBU_TEST_DB_URL")
	if dsn == "" {
		t.Skip("NEBU_TEST_DB_URL not set — skipping end-to-end forgery test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	var baselineCount int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log",
	).Scan(&baselineCount); err != nil {
		t.Fatalf("baseline count query failed: %v", err)
	}
	t.Logf("baseline audit_log count: %d", baselineCount)

	// Step 2: Attempt unauthenticated WriteAuditLog.
	client := dialUnauthenticatedCore(t)
	rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, rpcErr := client.WriteAuditLog(rpcCtx, &pb.WriteAuditLogRequest{
		ActorUserId: "forge-test-actor",
		Action:      "forgery_test_should_fail",
		Outcome:     "success",
	})

	if rpcErr == nil {
		t.Error("AC11 FAIL: unauthenticated WriteAuditLog succeeded — " +
			"core interceptor must reject unauthenticated calls")
	} else {
		st, ok := status.FromError(rpcErr)
		if ok && st.Code() != codes.Unauthenticated {
			t.Errorf("AC11 FAIL: expected Unauthenticated, got %v", st.Code())
		} else {
			t.Logf("AC11: RPC correctly rejected: %v", rpcErr)
		}
	}

	// Step 3: Verify no row was inserted.
	var afterCount int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log",
	).Scan(&afterCount); err != nil {
		t.Fatalf("post-attempt count query failed: %v", err)
	}

	if afterCount != baselineCount {
		t.Errorf("AC11 FAIL: audit_log row count changed from %d to %d — "+
			"a forged row was inserted despite the RPC rejection. "+
			"The interceptor must run BEFORE any DB write.", baselineCount, afterCount)
	} else {
		t.Logf("AC11 PASS: row count unchanged (%d) after unauthenticated RPC attempt", afterCount)
	}
}
