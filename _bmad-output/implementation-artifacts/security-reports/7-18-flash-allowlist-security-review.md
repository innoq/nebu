# Security Review – Story 7-18: Flash-Message Allowlist auf Admin GET-Handlern

**Reviewer:** Kassandra (hyper-critical security engineer)
**Date:** 2026-04-30
**Gate:** SEC Gate 1 – per-story (advisory before commit)
**Scope:** Staged diff for Story 7-18

## Files Reviewed

| File | Lines (added) | Type |
|---|---|---|
| `gateway/internal/admin/flash.go` | 29 | NEW |
| `gateway/internal/admin/flash_test.go` | 333 | NEW |
| `gateway/internal/admin/compliance_handler.go` | 6 mod | handler |
| `gateway/internal/admin/config.go` | 4 mod | handler |
| `gateway/internal/admin/role_mapping.go` | 4 mod | handler |
| `gateway/internal/admin/rooms.go` | 4 mod | handler |
| `gateway/internal/admin/users.go` | 2 mod | handler |
| `gateway/internal/admin/templates/components/alert_banner.html` | (unchanged sink) | template |
| `e2e/tests/features/admin/{compliance,config,role-mapping,room-detail}.spec.ts` | 10 mod | e2e |
| `gateway/internal/admin/{config,role_mapping,rooms_detail}_test.go` | 30 mod | unit |

---

## Classification: **CLEAN**

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 1 |
| INFO | 3 |

The fix is sound. Reflected XSS, social-engineering banner injection, and accidental DoS via oversized query strings are all mitigated. The allowlist is enforced at every reflection sink and the canonical POST→GET redirect strings line up exactly with the allowlist keys.

---

## Threat-Model Walkthrough

### 1. Allowlist bypass via URL encoding / Unicode / whitespace / null bytes

**Verdict: not exploitable.**

- `r.URL.Query().Get("flash")` performs one round of percent-decoding per Go stdlib. The resulting string is then byte-equality-compared to the map keys via `_, ok := allowedFlashMessages[msg]`.
- Go map lookup on `string` is **byte-exact**. There is no Unicode normalization (NFC/NFKC), case folding, whitespace trimming, or homoglyph matching.
- Probes that all correctly fail:
  - `flash=approved` (lowercase)         → not in map, rejected
  - `flash=Approved%20` (trailing space) → `"Approved "`, not in map, rejected
  - `flash=Approved%00` (null byte)      → `"Approved\x00"`, not in map, rejected
  - `flash=%41pproved` (percent-encoded `A`) → `"Approved"`, **accepted** (correctly equivalent — no security boundary crossed)
  - `flash=Approvеd` (Cyrillic `е` U+0435) → byte-distinct from `"Approved"`, rejected
  - `flash=Approved\nX-Injected:` (CRLF) → not in map, rejected (and `Get` would already reject decoding of raw CR/LF in well-formed clients)
- Even if a probe sneaks through Go's decoder, the value is rendered into `<span>{{ .Message }}</span>` of `alert_banner.html`. The template uses `html/template`, which contextually auto-escapes the text node. XSS is doubly blocked.

### 2. `len(msg) > 80` byte vs rune semantics

**Verdict: correct, but defense-in-depth only.**

- Go's built-in `len(string)` returns **bytes**, not runes. The author's intent ("80 characters") is therefore actually "80 bytes". Multi-byte UTF-8 will count more aggressively (e.g. an emoji = 4 bytes), which makes the cap *stricter* than rune-based, not looser. That is the safe direction for a length cap.
- The cap is functionally redundant: the longest allowlist key is `"Display name updated"` / `"Role mapping updated"` (20 bytes). Any value > 80 bytes was already going to fail the map lookup. Keeping the cap is reasonable defense-in-depth in case someone later adds a long string to the allowlist or refactors to substring matching.
- The cap fires **before** the map lookup, which avoids hashing arbitrarily long attacker input — a small but legitimate DoS hardening (`map[string]struct{}` lookup hashes the full string).
- Test coverage for the boundary exists (`TestSanitizeFlash_OversizedValueRejected`, `TestSanitizeFlash_ExactlyEightyCharsIsRejected`). Note: there is **no** explicit test for `len(msg) == 81` against an allowlist *prefix* shorter than 81 — but `OversizedValueRejected` covers exactly that with `"Config updated" + "x"*67`. Adequate.

### 3. Reflected output paths bypassing `sanitizeFlash`

**Verdict: complete coverage.**

`grep` for `\.Flash\|Flash:` across `gateway/internal/admin/*.go` (excluding tests) returns exactly five assignment sites:

| File | Line | Assignment |
|---|---|---|
| `users.go` | 94 | `if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != ""` |
| `config.go` | 24 | `if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != ""` |
| `role_mapping.go` | 31 | `if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != ""` |
| `rooms.go` | 92 | `if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != ""` |
| `compliance_handler.go` | 28 | `if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != ""` |

There are zero remaining call sites where `r.URL.Query().Get("flash")` is fed unsanitized into `AlertBannerData`. The `audit_log.html` template references `{{ if .Flash.Message }}` but `audit_log_handler.go` never assigns `Flash` (intentionally — see `page_data.go:256` "Flash is reserved for future flash messages (always zero-valued in read-only MVP)").

`TestFlash_AllFiveHandlersRejectUnknownFlash` covers all five sinks behaviorally.

### 4. Persistence / logging side-channels

**Verdict: not exploitable.**

- `grep -rn "RequestURI\|URL.Path\|URL.String\|RawQuery" gateway/` shows the only reads of full URL data are routing matches (`/admin/bootstrap`, `/admin/callback`) and template helpers — never logged with the query string attached.
- No middleware in `gateway/internal/middleware/` writes `r.URL.RawQuery` or `r.URL.String()` to logs.
- No DB write path touches the flash parameter.
- The flash value is therefore strictly an ephemeral, single-request, in-memory reflection. It cannot persist across sessions or land in audit logs.

### 5. Open-redirect via flash

**Verdict: not exploitable.**

- The flash value is interpolated into a text node (`<span>`), never into `href`, `action`, `src`, or any URL-context sink.
- The PRG redirects (`http.Redirect(w, r, ...)`) build the location with **string concatenation of constants and known IDs**, never with attacker-supplied flash data. Example: `"/admin/users/"+userID+"?flash=Role+updated"`. The flash is hardcoded server-side; the user-controlled portion is `userID`, which is its own concern (Story 7-22 covers IDOR/escaping for user IDs and is out of scope here).

### 6. Social-engineering residual risk

**Verdict: LOW – accept, document.**

An attacker who can phish an admin can still craft a link such as:

- `https://admin.example.com/admin/compliance?flash=Approved`
- `https://admin.example.com/admin/users/usr-999?flash=User+deactivated`

When the admin clicks it, the page will show a green success banner reading "Approved" or "User deactivated" even though no action took place. This is **banner spoofing**, not state change.

- Severity is LOW because:
  1. The actual page state (request status, user activation flag) is rendered separately from the live database, so the lie is immediately falsifiable by reading the row.
  2. The banner is dismissible and ephemeral.
  3. The set of possible lies is bounded to 11 short, generic phrases — all of which are routine admin outcomes, not coercive prompts ("Please re-authenticate at evil.example", "Your session has expired, log in again", etc., are NOT achievable).
  4. The 11 phrases do not contain credentials, links, or instructions — pure reassurance text.
- This is the explicit design tradeoff of using a query-string PRG flash rather than a server-stored flash. It is acceptable for MVP. If later threat-modeling tightens, move flash into a signed short-lived cookie or session entry.

### 7. XSS residual

**Verdict: not exploitable.**

Two independent mitigations stack:

1. **Allowlist:** None of the 11 strings contain `<`, `>`, `"`, `'`, `&`, or any JavaScript-relevant byte. Even if the template were swapped to `text/template`, no XSS payload is reachable.
2. **html/template auto-escaping:** `gateway/internal/admin/handler.go:5` imports `html/template`, and `alert_banner.html` interpolates via `{{ .Message }}` in HTML element-content context. Auto-escaping covers any future widening of the allowlist.

### 8. Canonical POST→GET redirect strings vs. allowlist

**Verdict: aligned — no orphaned strings.**

| POST handler emits | In allowlist? |
|---|---|
| `flash=Role+updated` (users) | yes |
| `flash=User+deactivated` (users) | yes |
| `flash=User+reactivated` (users) | yes |
| `flash=Display+name+updated` (users) | yes |
| `flash=Room+name+updated` (rooms) | yes |
| `flash=Room+archived` (rooms) | yes |
| `flash=Room+unarchived` (rooms) | yes |
| `flash=Config+updated` (config) | yes |
| `flash=Role+mapping+updated` (role_mapping) | yes |
| `flash=Approved` (compliance) | yes |
| `flash=Rejected` (compliance) | yes |

All 11 producers map 1:1 onto the 11 allowlist keys. No POST silently emits a string that the GET would drop. Confirmed by both `git diff --staged` and the renamed e2e expectations.

---

## Findings

### LOW-1 — Banner spoofing via crafted flash URL (residual social-engineering risk)

- **Where:** All 5 admin GET handlers reading `?flash=`.
- **Vector:** Phishing link such as `/admin/compliance?flash=Approved` shows a green success banner without a corresponding action.
- **Impact:** Admin may briefly believe an action succeeded; can be dispelled by inspecting the actual page data.
- **Recommendation (non-blocking):** Track as future hardening. Replace query-string PRG flash with a single-use, HMAC-signed cookie or a session-stored flash. Document explicitly in `docs/architecture/adr/` if the current model is the long-term choice.
- **Status for this story:** Acceptable. Story 7-18 was scoped to allowlist enforcement, and the residual risk is documented design.

### INFO-1 — Length cap is dead code under the current allowlist

- **Where:** `flash.go:24` (`if len(msg) > 80`).
- **Observation:** Longest allowlist key is 20 bytes; the cap is unreachable in practice.
- **Recommendation:** Keep as defense-in-depth. Optionally add a code comment explaining its forward-compatible purpose.

### INFO-2 — `len()` byte vs rune semantics

- **Where:** `flash.go:24`.
- **Observation:** `len(string)` returns byte count; the docstring says "80 characters". Because the allowlist contains ASCII only, this currently matters for nothing. If the allowlist ever gains UTF-8 entries, the cap will be stricter than a rune-based cap (safer direction).
- **Recommendation:** Either rename the comment to "80 bytes" for precision, or switch to `utf8.RuneCountInString` for symmetry with the docstring. Cosmetic only.

### INFO-3 — No CSP defense-in-depth claim verified for admin

- **Observation:** Out of scope for this story, but worth noting: a strict `Content-Security-Policy` on admin pages would reduce the residual blast radius of any future template-context bug.
- **Recommendation:** Confirm Story 5-14 (Security-Headers Middleware) emits a `script-src` policy that covers admin templates. Not gating for 7-18.

---

## Test Quality Notes

- `flash_test.go` covers: allowlist pass-through (parametric over the map), unknown rejection, oversize boundary, oversize-with-allowed-prefix, empty-string no-op, end-to-end rendering for each of the five GET handlers, end-to-end rejection of `flash=BAD` for each of the five GET handlers.
- **Missing (LOW priority for testing, NOT a security finding):** A dedicated probe for the `len(msg) == 81` boundary against an exact-length-81 allowlist prefix already exists implicitly in `OversizedValueRejected`, so coverage is fine.
- **Strength:** The integration tests assert *negative* presence (`!strings.Contains(body, "BAD")`) on the rendered HTML for all five handlers — this is the right shape for an XSS-defense test and would catch any future regression that re-introduces unsanitized reflection.

---

## Verdict

**CLEAN.** Ship it. The single LOW finding is a documented residual risk of the PRG-flash pattern, not a regression introduced by this story.

No CRITICAL or HIGH findings. No blocker for SEC Gate 1.
