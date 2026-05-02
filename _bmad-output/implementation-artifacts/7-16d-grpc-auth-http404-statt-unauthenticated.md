---
id: 7-16d
type: bugfix
security_review: required
created: 2026-04-30
---

# Story 7.16d: Bugfix — gRPC Auth: HTTP 404 statt Unauthenticated bei ungültigem Token

Status: ready-for-dev

## Story

As a security engineer,
I want the Elixir gRPC auth interceptor to return `codes.Unauthenticated` for invalid/missing tokens,
so that `TestCoreGRPC_RejectsUnauthenticatedDial` und `TestCoreGRPC_RejectsForgedToken` grün werden und die gRPC-Sicherheitsschicht verifizierbar ist.

## Context / Background

**Fehlende gRPC-Auth-Ablehnung (3 Test-Failures + 1 Folge-Fehler):**
```
grpc_auth_test.go:139: AC9 FAIL: expected codes.Unauthenticated, got Unimplemented
  (unexpected HTTP status code received from server: 404 (Not Found); malformed header: missing HTTP content-type)
grpc_auth_test.go:176: AC9 FAIL: expected codes.Unauthenticated for forged token, got Unimplemented
grpc_auth_test.go:286: AC11 FAIL: expected Unauthenticated, got Unimplemented
```

**Root Cause — Hypothese:**

Der Elixir `Nebu.Grpc.AuthInterceptor` ist korrekt implementiert (raises `GRPC.RPCError` mit `status: GRPC.Status.unauthenticated()`). Aber **beide** Fälle — gültiges und ungültiges Token — liefern das gleiche HTTP-404-Fehlerbild. Das deutet darauf hin, dass das Problem im HTTP/2-Layer liegt, nicht im Interceptor selbst:

1. **Interceptor-Error nicht als gRPC-Response serialisiert:** Wenn der Interceptor eine Exception wirft, könnte die `grpc` Elixir-Bibliothek (v0.11.5) den `GRPC.RPCError` nicht korrekt in einen HTTP/2 gRPC-Frame übersetzen → gRPC-go sieht HTTP 404 mit fehlendem `Content-Type: application/grpc`.

2. **Alternativ: `WriteAuditLog` nicht in der Service-Registry:** Wenn der gRPC-Endpoint den Service-Pfad `/core.CoreService/WriteAuditLog` nicht kennt, gibt er Unimplemented zurück **bevor** der Interceptor läuft. Zu prüfen: `Nebu.EventDispatcher.Server` implementiert korrekt `Core.CoreService.Service` und registriert `WriteAuditLog`.

3. **Alternativ: h2c-Negotiation schlägt fehl:** gRPC-go verbindet sich über h2c (HTTP/2 cleartext); wenn Cowboy/Ranch nicht auf h2c konfiguriert ist, erhält gRPC-go HTTP/1.1-Responses.

**Zu verifizieren (als erster Schritt in der Implementierung):**
- Führe einen manuellen `grpcurl` gegen `core:9000` aus (im Docker-Netz):
  ```bash
  docker compose exec gateway sh -c \
    "grpcurl -plaintext -d '{}' core:9000 core.CoreService/WriteAuditLog"
  ```
  Erwartung mit fehlendem Token: gRPC status `UNAUTHENTICATED`
  Ist-Zustand: `Unimplemented` oder HTTP 404

- Prüfe `Nebu.EventDispatcher.Server` — `use Core.CoreService.Service` muss `write_audit_log/2` exportieren.

## Acceptance Criteria

1. `grpc.WithTransportCredentials(insecure.NewCredentials())` + kein Token → Core gibt `codes.Unauthenticated` zurück (nicht `Unimplemented` oder HTTP 404).

2. Gefälschtes Token (nicht PSK) → Core gibt `codes.Unauthenticated` zurück.

3. Gültiges PSK-Token → Core gibt `codes.Unimplemented` oder einen Domänen-Fehler zurück (nicht `codes.Unauthenticated`) — der Interceptor passiert, `WriteAuditLog` kann antworten wie er möchte.

4. `TestCoreGRPC_RejectsUnauthenticatedDial` grün.

5. `TestCoreGRPC_RejectsForgedToken` grün.

6. `TestAuditForgery_NoRowInserted` grün (prüft: ungültiges Token → kein DB-Row inserted).

7. `TestCoreGRPC_AcceptsValidToken` bleibt grün (gibt bereits PASS).

8. Elixir Unit-Tests für `Nebu.Grpc.AuthInterceptor` bleiben grün (3/4 Tests in `event_dispatcher`).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Integration: TestCoreGRPC_RejectsUnauthenticatedDial] — Go integration (existierender Test)
   - Given: gRPC-Verbindung zu core:9000, kein Token
   - When: WriteAuditLog RPC
   - Then: codes.Unauthenticated

2. [Integration: TestCoreGRPC_RejectsForgedToken] — Go integration (existierender Test)
   - Given: gRPC-Verbindung zu core:9000, Token="forged-token-..."
   - When: WriteAuditLog RPC
   - Then: codes.Unauthenticated

3. [Integration: TestAuditForgery_NoRowInserted] — Go integration (existierender Test)
   - Given: ungültiges Token
   - When: WriteAuditLog RPC
   - Then: kein audit_log-Row in DB

4. [Elixir Unit: AuthInterceptor rejects missing token] — ExUnit (existierend)
   - Given: kein x-nebu-node-token Header
   - When: Interceptor.call()
   - Then: raises GRPC.RPCError mit UNAUTHENTICATED

## Implementation Notes

**Zu untersuchende Dateien:**
- `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex` — Interceptor-Implementierung (korrekt, vermutlich nicht die Ursache)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` — `intercept` + `run` Konfiguration
- `core/apps/event_dispatcher/lib/nebu/event/application.ex` — `GRPC.Server.Supervisor` Startup
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `use Core.CoreService.Service` + `write_audit_log/2`
- `core/mix.exs` oder `apps/event_dispatcher/mix.exs` — `grpc` Version (aktuell `~> 0.8`, installiert `0.11.5`)

**Mögliche Fixes:**
- A: Sicherstellen dass `GRPC.RPCError` korrekt als HTTP/2 gRPC-Frame serialisiert wird — ggf. `grpc` auf aktuelle Version bumpen
- B: Sicherstellen dass `write_audit_log` im Server korrekt registriert ist und der Interceptor VOR dem Routing läuft
- C: Cowboy `protocol_options: [:http2]` explizit konfigurieren wenn h2c-Negotiation Problem

**Security-Gate 1 (per-story):** Pflicht. Auth-Interceptor ist Kern der gRPC-Sicherheitsschicht.
