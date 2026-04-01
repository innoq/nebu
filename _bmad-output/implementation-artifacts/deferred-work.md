# Deferred Work

Items deferred during code review. Each entry references the source story and the reason for deferral.

---

## Deferred from: code review of 3-10-oidc-callback-handler-session-cookie (2026-04-01)

- **GET /admin/logout has no CSRF protection** — Spec mandates GET; SameSite=Strict on session cookie provides reasonable cross-site protection. Formal CSRF hardening (e.g. POST + token) should be addressed in a dedicated security review story.
- **Legacy-route Cookie-Path-Mismatch** — `LoginHandler` sets `admin_oidc_state` with `Path=/admin/auth`; `CallbackHandler` deletes with `Path=/admin`. The legacy cookie under `/admin/auth` is not cleaned up. Pre-existing from Story 3.9; will be resolved when legacy routes (`GET /admin/auth/login`, `GET /admin/auth/callback`) are removed.

## Deferred from: code review of 3-11-admin-session-middleware-cookie-validation (2026-04-01)

- **INFO: Inkonsistentes Logging in SessionGuard** — Fehlende Cookie und Signatur-Fehler werden nicht geloggt (nur Redirect), waehrend JSON-Parse-Fehler korrekt mit `slog.Warn` geloggt werden. Stylistic consistency, kein Bug.
- **INFO: Duplikat-Logik verifySessionCookie vs AdminAuth.verifyCookie** — Beide Funktionen implementieren identische HMAC-Verifikation. Refactoring zu einer shared Funktion wuerde Wartungsrisiko reduzieren. Bewusste Architektur-Entscheidung per Story-Spec (Option A).
- **INFO: Leeres Secret nicht validiert** — `SessionGuard` prueft nicht ob `secret` leer ist. In Praxis durch `main.go` PSK-Read und `strings.TrimSpace` abgesichert. Defense-in-depth Improvement fuer spaetere Security-Haertung.
