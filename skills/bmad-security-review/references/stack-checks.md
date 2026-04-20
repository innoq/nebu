---
name: stack-checks
description: Stack-specific vulnerability patterns for Go, Elixir/OTP, and PostgreSQL.
---

# Stack-Specific Checks

Load only the section that matches the language of the staged diff.

## Go (gateway/, media/)

**Crypto**
- `math/rand` used for tokens, session IDs, nonces, or any security-relevant value → must be `crypto/rand` (CWE-338)
- `md5.Sum`, `sha1.Sum` for integrity or authentication → CWE-327
- `tls.Config{InsecureSkipVerify: true}` anywhere that is not a well-flagged test helper → CWE-295
- `rsa.GenerateKey` with bits < 2048 → CWE-326
- AES-GCM: nonce reuse, or nonce derived from predictable input → CWE-323

**Injection**
- String-concatenated SQL in `db.Query` / `db.Exec` / `db.QueryRow` → CWE-89. Only parameterized queries are acceptable.
- `exec.Command(cmd, userInput...)` or `exec.CommandContext(...)` with attacker-controllable args → CWE-78
- `template.HTML(userInput)`, `template.JS(userInput)`, `template.URL(userInput)` — bypasses Go's auto-escape → CWE-79
- Raw HTML assembled via `fmt.Sprintf` and returned via `w.Write` → CWE-79

**Goroutine & resource hygiene**
- `go func() { ... }()` per request without semaphore or worker pool → unbounded goroutine growth (CWE-400)
- `io.ReadAll(r.Body)` without `http.MaxBytesReader` wrapping → unbounded memory (CWE-400)
- `regexp.MustCompile` where the pattern is user-controlled → ReDoS (CWE-1333)
- Missing `context.Context` propagation on long-running operations — timeout bypass

**HTTP handlers**
- `http.Redirect` to `r.URL.Query().Get("return")` or similar without allowlist → CWE-601
- Missing rate-limit middleware on `/login`, `/register`, `/reset` equivalents → CWE-307
- Errors returned verbatim via `fmt.Fprintf(w, "%v", err)` → CWE-209 info disclosure
- Missing or misconfigured security headers (`X-Content-Type-Options`, `Strict-Transport-Security`, `Content-Security-Policy`) on admin UI
- `w.Header().Set("Access-Control-Allow-Origin", "*")` on authenticated endpoints → CORS misconfiguration

**SSRF**
- `http.Get(userURL)`, `http.Post(userURL, ...)` without host allowlist → CWE-918
- Outbound HTTP client that does not explicitly block `169.254.169.254` / `metadata.google.internal` / `localhost` / `10./172.16./192.168.` when called with user input

**Serialization**
- `json.Unmarshal` into a map with unbounded depth / no size limit → DoS
- `encoding/gob` on untrusted input → CWE-502
- XML parsing with `DefaultSettings` that does not disable external entities → CWE-611

## Elixir / OTP (core/)

**Atom exhaustion**
- `String.to_atom(user_input)` — atoms are never GC'd, unbounded creation is DoS (CWE-400). Must be `String.to_existing_atom/1` with a `rescue ArgumentError`.

**Dynamic code execution**
- `apply(module, function, args)` where any argument comes from external input → CWE-94
- `Code.eval_string/1`, `Code.eval_file/1`, `Code.eval_quoted/1` on anything attacker-influenced → CWE-95
- `:erlang.binary_to_term(data)` without the `[:safe]` option → CWE-502

**Distribution**
- Hard-coded cookie values in source or `.erlang.cookie` leaked in logs → cluster compromise
- `Node.connect/1` to an untrusted peer without TLS distribution (`inet_tls_dist`)

**GenServer / mailbox**
- `GenServer.cast/2` into a worker whose `handle_cast/2` blocks or is slow → unbounded mailbox growth
- Pattern match in `handle_info/2` that does not have a catch-all → messages silently pile up on shape drift

**Crypto**
- `:crypto.hash(:md5, ...)` or `:sha` for auth / integrity → CWE-327
- `:public_key.decrypt_private/2` without padding-scheme pinning → padding oracle risk
- Ed25519 verify skipped or wrapped in `try/rescue` that swallows the failure → CWE-347

**Ecto**
- Raw SQL via `Ecto.Adapters.SQL.query!/3` with string interpolation → CWE-89
- `Ecto.Changeset` without `validate_required/3` on sensitive fields → input-validation gap
- Direct `Repo.insert!/1` that bypasses changeset validation

**Logger**
- `Logger.info("user: #{inspect(user)}")` where `user` carries tokens / passwords → secret leak (CWE-532)

## PostgreSQL (migrations, SQL, RLS)

**Row Security Policies (RSP)**
- New table that stores user-scoped or compliance data without `ENABLE ROW LEVEL SECURITY` → CRITICAL for Nebu
- `ENABLE ROW LEVEL SECURITY` but `FORCE ROW LEVEL SECURITY` missing → bypass by table owner / superuser
- Policy defined with `USING (true)` or an overly permissive predicate → effective bypass
- Policy applied to SELECT but missing on INSERT / UPDATE / DELETE → one-sided protection

**Privilege escalation**
- `CREATE FUNCTION ... SECURITY DEFINER` without `SET search_path = pg_catalog, public` → CWE-426 / search-path hijack
- `GRANT ALL ON SCHEMA sensitive TO PUBLIC` or to a broad application role
- `ALTER TABLE ... OWNER TO` handing ownership to a role that should not have DDL rights

**Injection at DB layer**
- Dynamic SQL built via `format()` inside PL/pgSQL with string concatenation and `EXECUTE`
- `EXECUTE concat(...)` where the concatenated value includes a parameter

**Migration hygiene**
- Migrations that drop columns containing compliance data without an audit-log entry
- Migrations that `ALTER TABLE audit_log` to grant UPDATE / DELETE → violates audit-immutability invariant
- `DROP TABLE` or destructive change without a rollback path documented
- Missing `NOT NULL` on columns that act as tenant / user isolators used by RLS policies