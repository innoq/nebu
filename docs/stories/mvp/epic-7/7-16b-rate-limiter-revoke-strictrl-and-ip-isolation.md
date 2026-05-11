---
id: 7-16b
type: bugfix
security_review: required
created: 2026-04-30
---

# Story 7.16b: Bugfix — Rate Limiter: revoke-Session fehlt strictRL + IP-Bucket-Isolation kaputt

Status: ready-for-dev

## Story

As a security-conscious developer,
I want the revoke-session endpoint wrapped in `strictRL` and the integration-test IP isolation to work correctly,
so that `TestComplianceRoutes_RateLimited_429` alle 9 Sub-Tests besteht und keine falschen 429s produziert.

## Context / Background

**Bug 1 — Revoke-Session fehlt strictRL (Story 5.29b AC1 Gap):**

`POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` (main.go:907) ist in `adminRL` (burst=20) statt `strictRL` (burst=10) gewickelt. Story 5.29b AC1 verlangt explizit `strictRL` für diesen Endpoint (Kommentar in `compliance_rate_limit_test.go:86`: „MINOR-1 (TEA): revoke-session is also in scope for AC1's strictRL wrap"). Das führt zur Failure:
```
11th request: expected 429 Too Many Requests (strictRL), got 403
```

**Bug 2 — IP-Bucket-Isolation bricht bei `trustedProxy=false`:**

`strictRL` extrahiert die Client-IP aus der TCP-Verbindung wenn `trustedProxy=false` (kein `NEBU_TRUSTED_PROXY=true` im Makefile). Alle Requests vom Test-Container kommen von derselben IP → teilen sich ein Rate-Limit-Bucket. Das führt zu frühzeitigen 429s bei Request 2–3 (statt erst nach 11):
```
compliance_rate_limit_test.go:136: request 2: got premature 429 before burst exhausted (burst should be 10)
```

Nebeneffekt: `TestIDOR_PermissionDenied_Returns403` bekommt 429 auf `/login` weil das `strictRL`-Bucket durch Compliance-Tests erschöpft ist.

**Zwei unabhängige Fixes, in einer Story weil sie denselben Codepfad betreffen:**

Fix A — Revoke-Session in `strictRL` wrappen (main.go:907–908):
- Ersetze `adminRL(...)` mit `strictRL(bodyLimit64KiB(csrf(sessionGuard(...)))))` für den Revoke-Endpoint.

Fix B — `NEBU_TRUSTED_PROXY=true` in `test-integration` Makefile + eigene `complianceRL`-Instanz:
- Option 1 (einfacher): `NEBU_TRUSTED_PROXY=true` als Env-Var in `make test-integration` übergeben. Dann respektiert der Rate-Limiter den `X-Forwarded-For: uniqueIP`-Header, den der Test setzt.
- Option 2 (sauberer): Separate `complianceRL`-Instanz für Compliance-Endpoints (statt `strictRL` mit Login zu teilen). Gleiche Rate/Burst-Parameter, aber eigener LRU-Cache → kein Bucket-Sharing zwischen Login und Compliance.

**Empfehlung:** Option 2, weil es auch im Produktionsbetrieb Sinn ergibt: Login-Brute-Force und Compliance-Rate-Limits sind unabhängige Sicherheitszonen.

## Acceptance Criteria

1. `POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` (main.go ~907) ist in `strictRL` statt `adminRL` gewickelt — `bodyLimit64KiB + csrf + sessionGuard` bleiben erhalten.

2. Für Compliance-Endpoints existiert eine eigene `complianceRL`-Instanz (`NewIPRateLimiter` mit Rate=10/min, Burst=10, Label="compliance") — entkoppelt von `strictRL` (Login).

3. `TestComplianceRoutes_RateLimited_429` — alle 9 Sub-Tests:
   - Requests 1–10: kein 429 (unabhängig von anderen laufenden Tests)
   - Request 11: 429 M_LIMIT_EXCEEDED

4. `TestIDOR_PermissionDenied_Returns403` schlägt nicht mehr an einem Rate-Limit auf dem Login-Endpoint fehl.

5. Login-Endpoint (`/_matrix/client/v3/login`) bleibt in `strictRL` — Änderung betrifft nur Compliance-Endpoints.

6. Unit-Tests für `middleware.NewIPRateLimiter` bleiben grün.

7. Security Review bestätigt: `complianceRL` hat identische Rate/Burst-Charakteristik wie bisher (`strictRL`), keine Rate-Limit-Umgehung möglich durch den Wechsel.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Integration: TestComplianceRoutes_RateLimited_429/POST_/access-requests] — Go integration
   - Given: Unique `X-Forwarded-For` IP für diesen Sub-Test
   - When: 10 Requests → alle non-429; 11. Request
   - Then: 429 M_LIMIT_EXCEEDED

2. [Integration: TestComplianceRoutes_RateLimited_429/POST_/admin/compliance/sessions/{sessionId}/revoke] — Go integration
   - Given: Unique `X-Forwarded-For` IP
   - When: 10 Requests → alle non-429; 11. Request
   - Then: 429 M_LIMIT_EXCEEDED (jetzt mit strictRL statt adminRL)

3. [Integration: TestIDOR_PermissionDenied_Returns403] — Go integration
   - Given: `complianceRL` entkoppelt von `strictRL`
   - When: Login-Request für kai@example.com
   - Then: HTTP 200 (kein falsches 429)

*(Die existierenden Tests in `compliance_rate_limit_test.go` und `idor_test.go` sind die Specs.)*

## Implementation Notes

**Dateien:**
- `gateway/cmd/gateway/main.go` — `complianceRL` neu anlegen (~Zeile 210), Revoke-Endpoint von `adminRL` auf `complianceRL` wechseln (Zeile 907), alle anderen Compliance-Endpoints von `strictRL` auf `complianceRL` wechseln (Zeilen 803–929)
- Makefile — kein Change nötig wenn Option 2 (eigene Instanz) gewählt; `NEBU_TRUSTED_PROXY=true` nur als Fallback

**Security-Gate 1 (per-story):** Pflicht. Änderung betrifft Rate-Limiting-Sicherheitsschicht.
