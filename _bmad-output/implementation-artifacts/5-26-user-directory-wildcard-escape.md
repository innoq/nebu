---
security_review: required
---

# Story 5.26: User Directory Search — LIKE Wildcard Escape + Input Validation

Status: ready-for-dev

## Story

As an authenticated user,
I want `POST /_matrix/client/v3/user_directory/search` to reject wildcard-only queries and escape LIKE metacharacters,
so that any authenticated user cannot dump the full user table by passing `%` as the search term.

---

## Background / Motivation

Security audit (2026-04-20): `cmd/gateway/main.go:358–394` executes `ILIKE $1` with `fmt.Sprintf("%%%s%%", req.SearchTerm)`. If an attacker passes `%` or `_` (both LIKE metacharacters) or empty string, the query matches every row — full user-enumeration.

Also: `uid[1:strings.Index(uid, ":")]` panics if `uid` contains no `:` (IndexOf returns -1, slice bounds-out-of-range). Currently triggered only by malformed internal data but a latent crash.

---

## Acceptance Criteria

1. `SearchTerm` validation:
   - Trim leading/trailing whitespace
   - Reject if empty → 400 `M_INVALID_PARAM`
   - Reject if length < 2 (minimum 2 characters) → 400
   - Reject if length > 64 → 400

2. LIKE-metachar escape: `%` → `\%`, `_` → `\_`, `\` → `\\` before wrapping `%…%`. Use `strings.NewReplacer`.

3. Apply the SQL `ESCAPE '\'` clause on the `ILIKE` expression: `WHERE display_name ILIKE $1 ESCAPE '\'`.

4. Fix the panic: `i := strings.IndexByte(uid, ':'); if i <= 0 { continue }` before slicing.

5. Result cap enforced: if `req.Limit > 100` → clamp to 100. `req.Limit == 0` → default 10.

6. Rate-limit this endpoint via the authenticated-user tier (out of scope for Story 5.21 which covers unauthenticated only — coordinate: add a second authenticated-user rate-limit tier in 5.21 or follow-up).

7. Unit tests:
   - `TestUserDirectory_RejectsWildcardOnlyInput`
   - `TestUserDirectory_EscapesPercentUnderscore`
   - `TestUserDirectory_RejectsEmpty`
   - `TestUserDirectory_NoPanic_OnMalformedUID`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestUserDirectory_SearchTerm_Percent_Returns400` — `{"search_term":"%"}` → 400

2. `TestUserDirectory_SearchTerm_Alice_MatchesLiteral` — Given a user `alice%test`, searching for `alice%` should return 0 rows (the `%` is escaped), searching for `alice` should return the row

3. `TestUserDirectory_NoPanic_OnMissingColon` — insert row with `users.user_id='noformat'` (only possible via bypassing constraints in test); handler does not panic, skips the row

---

## Implementation Notes

- `strings.NewReplacer(`\\`, `\\\\`, `%`, `\\%`, `_`, `\\_`)` — careful with backslash escaping
- `ESCAPE '\'` on the SQL side is PostgreSQL-specific and works with pgx
- The rate-limit coordination with Story 5.21 is a cross-reference; document in PR which story owns the authenticated-user tier
