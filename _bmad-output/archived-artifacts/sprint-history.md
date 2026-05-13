# Sprint History Log

> Archived: 2026-05-13. Extracted from sprint-status.yaml.

# phase-3-planning: Epic 13 (Deployment), Epic 14 (Admin: Claim-Lock/OIDC-Import/GDPR), Epic 15 (Spaces + Moderator): 2026-05-13
# story 11-11 done (pipeline: ATDD+Code CLEAN 1 cycle+noproc guard added): 2026-05-12
# story 11-10 done (pipeline: ATDD+Dev+Code CLEAN (3 cycles, 3 MAJOR fixed: configReader, claims wired, TTL cache)+UX 6 violations fixed+Kassandra CLEAN): 2026-05-12
# story 11-10 created: 11-10 → ready-for-dev: 2026-05-12 (OIDC Claim Mapping — Bootstrap Wizard step 3 + Admin UI settings page + FormatUserIDFromClaims refactor; security_review required)
# story 11-9 done (pipeline: ATDD+Dev+Code CLEAN (2 cycles, 2 MAJOR fixed: error-page footer, make redeploy args) + UX gate ADV-1/ADV-2 fixed + Kassandra skipped (not-needed)): 2026-05-12
# story bug-thread-redeliver done (pipeline: Bug fix: thread root re-delivery in incremental sync): 2026-05-11
# story 11-8 done (pipeline: ATDD+Dev+Code CLEAN (2 cycles) + Kassandra CLEAN): 2026-05-11
# story 11-7 done (pipeline: ATDD+Code CLEAN 2 cycles+Kassandra CLEAN): 2026-05-11
# story 11-6 done (pipeline: ATDD+Code CLEAN — 5 Godog + 3 Playwright E2E search tests): 2026-05-11
# story 11-6 created: 11-6 → ready-for-dev: 2026-05-11 (Gherkin E2E Search Flow — 5 Godog scenarios + 3 Playwright+Cucumber scenarios; security_review not-needed)
# story 11-5 done (pipeline: ATDD+Dev+Code CLEAN; Kassandra CLEAN (0 findings)): 2026-05-11
# story 11-4 done (pipeline: ATDD+Dev+Code CLEAN (1 cycle, 2 MINOR fixed); Kassandra CLEAN): 2026-05-11
# story 11-3 done (pipeline: ATDD+Dev+Code CLEAN; Kassandra CLEAN (MEDIUM offset-cap fixed inline)): 2026-05-08
# story 11-3 created: 11-3 → ready-for-dev: 2026-05-08 (SearchMessages gRPC Handler — proto SearchMessages RPC + SearchResult + ProfileInfo, server.ex handler, user_id from metadata (Kassandra M-2), FakeSearchDB injection, 6 acceptance tests; security_review required)
# story 11-2 done (pipeline: ATDD+Code CLEAN 2 cycles+Kassandra CLEAN (2 MEDIUM fixed inline: state_key NULL + IDOR docstring)): 2026-05-08
# story 11-2 created: 11-2 → ready-for-dev: 2026-05-08 (Search Membership Enforcement — Nebu.Search.DB + canonical SQL contract; security_review required)
# story 9-30 done (pipeline: ATDD+Code CLEAN 2 cycles+Kassandra CLEAN): 2026-05-08
# story 9-29 done (pipeline: ATDD+Dev+Code CLEAN 1 cycle (MINORs fixed)+Kassandra CLEAN): 2026-05-08
# story 9-28 done (pipeline: ATDD+Dev+Code+Kassandra CLEAN — 1 cycle (MAJOR-1 noproc pattern, MAJOR-3 index, MINOR-1 event_in_room?)): 2026-05-08
# story 11-1 done (pipeline: ATDD+Code CLEAN — tsvector GIN index, trigger, backfill; ADR-010 accepted): 2026-05-08
# story 9-27 done (pipeline: ATDD+Code CLEAN 3 cycles — MatchError→500 fixed, §11.35.1 compliant, Kassandra CLEAN): 2026-05-08
# story 9-26 done (pipeline: ATDD+9MAJ fixed(9-26a)+IndexedDB fix(9-26b)+AdminBootstrap(9-26c)+Code CLEAN 3 cycles): 2026-05-07
# story 9-26 done (pipeline: ATDD+Dev+Test-Review CLEAN — 14/15 pass (1 skip: bootstrap-wizard correctly skipped on non-fresh DB), M-1 regression stable; 13 BUG-E2E fixes including BUG-E2E-11 core auto-bootstrap preemption; security_review not-needed): 2026-05-07
# story 9-26 created: Element Web E2E Browser-First Suite (Phase 1: Framework + Phase 2: 8 Feature-Specs; security_review not-needed): 2026-05-07
# epic-9 SEC Gate 2 final (stories 9-19..9-25) — CLEAN (0 HIGH, 2 MEDIUM non-blocking: forgotten_rooms RLS + querySinceTsMs device-unaware; follow-ups needed in epic-10): 2026-05-06
# story 9-25 done (pipeline: ATDD+Dev+Code-Review CLEAN 1 cycle (3 MINOR doc fixes); security_review not-needed): 2026-05-06
# story 9-24 done (pipeline: ATDD+Dev+Test-Review 2 MAJOR fixed+Code-Review CLEAN+RLS fix in CI; security_review not-needed): 2026-05-06
# story 9-23 done (pipeline: ATDD+Dev+Test-Review CLEAN+Code-Review CLEAN; security_review not-needed): 2026-05-06
# story 9-22 done (pipeline: 4 MAJOR fixed 3 cycles+Code CLEAN; security_review not-needed): 2026-05-06
# story 9-21 done (pipeline: ATDD+Dev+Test-Review CLEAN+Code-Review CLEAN; security_review not-needed): 2026-05-06
# story 9-20 done (pipeline: ATDD+Dev+Test-Review CLEAN+Code-Review CLEAN; security_review not-needed): 2026-05-06
# story 9-20 created: 9-20 → ready-for-dev: 2026-05-06 (GAP-PREV-BATCH — delta sync missing prev_batch when limited:true; fix fetch_delta_rooms to derive prev_batch from List.first(events)["event_id"]; security_review not-needed)
# story 9-19 done (pipeline: ATDD+Dev+Test-Review (1 MAJOR fixed: buildInviteRooms forgotten exclusion)+Code-Review (3 MINOR auto-fixed: DEBUG logs, forgotten_rooms error log, polling loop replaced)+security_review not-needed): 2026-05-06
# story 9-18 done (pipeline: ATDD+Dev+Test-Review (1 MAJOR fixed: AC5 gRPC-error test)+Code-Review (2 MAJOR fixed: dedup state_key bug+ExUnit coverage; 2 MINOR fixed)+Kassandra CLEAN (0 HIGH, 4 INFO)): 2026-05-05
# story 9-17 done (pipeline: ATDD+Dev+Test-Review CLEAN (0 MAJOR)+Code-Review CLEAN (2 MINOR fixed: t.Parallel+maxMembersLimit const); security_review not-needed): 2026-05-05
# story 9-16 done (pipeline: ATDD+Dev+Test-Review CLEAN+Code-Review (1 MAJOR fixed: join_rules seed step added) security_review not-needed): 2026-05-05
# story 9-15 done (pipeline: ATDD+Dev+Test-Review CLEAN (0 MAJOR)+Code-Review (1 MAJOR inline: test-regression, 3 MINOR inline: confirm-dialog fallback, avatar initial, story File List); security_review not-needed): 2026-05-05
# story 9-15 created: 9-15 → ready-for-dev: 2026-05-05 (Admin UI Bug-Fixes — Select-Dropdown-Sichtbarkeit bg-base-200, Compliance-Button-Kontrast btn-outline, Room-Fallback-Name (Direct Chat · N members); security_review not-needed)
# story 9-14 done (pipeline: ATDD+Dev+Test-Review (4 MAJOR fixed: AC3 encrypt test, AC5 pre-expiry, AC8 no-session, AC9 audit)+Code-Review CLEAN+Kassandra CLEAN (0 HIGH, 2 MEDIUM non-blocking); OIDC silent refresh, AES-GCM RT storage; security_review required): 2026-05-05
# story 9-13 done (pipeline: ATDD+Dev+Test-Review (2 MAJOR fixed: AC9 missing test + AC14 badge-info path)+Code-Review CLEAN; 17 UI/UX fixes: logo, nav, SSE guard, btn-error, border-l-4, badges, timestamps; security_review not-needed): 2026-05-05
# story 9-12 done (pipeline: ATDD+Dev+Test-Review (3 MINOR fixed: redundant-grep, precise-60d-check, trap-cleanup)+Code-Review CLEAN; CI gate hardened allow_failure→false, verify-docs 60d threshold, bmad-maintain-arc42 skill, CLAUDE.md gate; security_review not-needed): 2026-05-05
# story 9-11 done (pipeline: pre-committed impl (commit 5b41398)+Test-Review (3 MINOR: story-ref×5, DRY accepted, T1 dual-concern)+Code-Review (5 MINOR fixed: story-refs, make-setup facts, ADR-002 ref, building-blocks compliance/, DRY comment); security_review not-needed): 2026-05-05
# story 9-10b done (pipeline: ATDD(pre-existing stubs from 9-10a)+Dev+Test-Review (2 MINOR fixed: matrixURL, vacuous-pass note)+Code-Review CLEAN; unsigned.age fix in sync.go + 10 Godog step defs; security_review not-needed): 2026-05-05
# story 9-10b created: 9-10b → ready-for-dev: 2026-05-05 (Matrix Event Correctness Godog Scenarios & Fixes — implement 10 new Godog step defs, fix unsigned.age HIGH deviation in sync.go; 1 production fix + step infra for all 6 scenarios green)
# story 9-10a done (pipeline: ATDD+Dev+Code-Review CLEAN — audit doc + 6 Godog stubs; 1 HIGH DEVIATION (unsigned.age missing from timeline events), 3 PASS; security_review not-needed): 2026-05-05
# story 9-10a created: 9-10a → ready-for-dev: 2026-05-05 (Matrix Event Correctness Spike — DM-loop root cause investigation: /agent-oracle audit of keys/query format, m.room.encryption, unsigned.age, device_lists/OTK count; audit doc + failing Godog stubs for 9-10b)
# story 9-9 done (pipeline: ATDD+Dev+Test-Review (2 MAJOR fixed: FAILED_PRECONDITION ExUnit+double-archive AC5)+Code-Review CLEAN (6 INFO only)+Kassandra CLEAN (0 HIGH, 1 MEDIUM non-blocking)): 2026-05-05
# story 9-9 created: 9-9 → ready-for-dev: 2026-05-05 (Archive TOCTOU Fix — SELECT FOR UPDATE in send_event, FAILED_PRECONDITION propagation, gateway FailedPrecondition→403 M_ROOM_ARCHIVED; SEC Gate 2 HIGH-2 follow-up from epic-6)
# story 9-8 done (pipeline: ATDD+Dev+Test-Review (5 MAJOR fixed: AC4+ExUnit+crash/restart+ordering+audit)+Code-Review (3 MAJOR fixed: m.room.name lost+predecessor missing+create-before-member order)+Kassandra HIGH-1 fixed (requester_id from gRPC metadata); 3 MEDIUM follow-up stories): 2026-05-05
# story 9-8 created: 9-8 → ready-for-dev: 2026-05-05 (Room Version Upgrade — new UpgradeRoom gRPC RPC, tombstone+new room+state copy+invitations, capabilities version 10)
# story 9-7 done (pipeline: ATDD+Dev+Test-Review (3 MAJOR fixed: AC3 sync+AC5 GET/state+Elixir state_key test)+Code-Review (2 MAJOR fixed: txn_id dedup+state_key NULL)+Kassandra CRITICAL fixed (state events `:change_state` power level)): 2026-05-05
# story 9-7 created: 9-7 → ready-for-dev: 2026-05-05 (Room State Event Types Full Implementation — replace 501 fallback with SendEvent gRPC, add state_key to proto, extend build_state_events)
# story 9-6 done (pipeline: ATDD+Dev+Test-Review CLEAN+Code-Review (1 MINOR gofmt)+Kassandra CLEAN): 2026-05-04
# story 9-6 created: 9-6 → ready-for-dev: 2026-05-04
# story 9-5 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR)+Code-Review (2 HIGH fixed: four-eyes self-approval + note length cap)+Kassandra CLEAN): 2026-05-04
# story 9-5 created: 9-5 → ready-for-dev: 2026-05-04
# story 9-4 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR)+Code-Review (1 MEDIUM fixed: flash allowlist)+Kassandra CLEAN): 2026-05-04
# story 9-4 created: 9-4 → ready-for-dev: 2026-05-04
# story 9-3 done (pipeline: ATDD+Dev+Test-Review (2 MAJOR fixed: Playwright strict-mode selector + audit archive test)+Code-Review (1 MAJOR fixed: UpdateRoomNameHandler 404)+Kassandra HIGH-1-fixed (actor_id via contextWithAdminIdentity; 6 identity-propagation tests added)): 2026-05-04
# story 9-3 created: 9-3 → ready-for-dev: 2026-05-04
# story 9-2 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 5 MINOR; M-1 loginAsAdmin extracted + M-3 double login removed)+Code-Review CLEAN+Kassandra HIGH-1-fixed (audit log: deactivate/reactivate/role in Core server.ex + audit_writer allowlist)): 2026-05-04
# story 9-2 created: 9-2 → ready-for-dev: 2026-05-04
# story 9-1 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 5 MINOR; M-4 nil stubs fixed inline)+Code-Review (5 MAJOR SQL bugs fixed: nonce cols, room_members table, bigint epoch, set_at, @spec)+Kassandra CLEAN (3 MEDIUM, 1 LOW)): 2026-05-04
# story 9-1 created: epic-9 → in-progress, 9-1 → ready-for-dev: 2026-05-04
# story 8-11 done (pipeline: ATDD+Dev+Code CLEAN — WaitAndRunMigrations retry loop, wait-for-stack.sh, integration-test-k8s job): 2026-05-04
# story 8-10a done (pipeline: ATDD+Dev+Test-Review (2 MAJOR fixed inline: AC4b sync wakeup test + AC5b E2E-only note, 2 MINOR fixed: pg.leave on_exit + ollama unstage)+Code-Review CLEAN (1 MINOR inline: unrelated script unstaged)+Kassandra CLEAN (0 MEDIUM, 2 INFO)): 2026-04-29
# story 8-10a created (pre-release bug fixes: invite :pg notification + profile upsert on login): 2026-04-29
# strategic pivot (2026-04-28): dual-host publication — github.com/innoq/nebu (GitHub Actions) + gitlab.opencode.de/nebu/nebu-server (GitLab CI). 8-6 NOT skipped. 8-7 expanded to .github/ + .gitlab/ templates. 8-10 pushes to both remotes. Retro-fix for GitHub-only references in 8-1/8-3/8-4 deferred to dedicated chore commit before 8-10.
# story 8-9 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 3 MINOR; M-1 mode-field assertion + INFO-2 umlaut fixed inline)+Code-Review (3 MINOR markdownlint fixed inline)+Kassandra CLEAN (2 MEDIUM, M-1 --quick warning banner fixed inline)): 2026-04-28 — pipeline stops here per maintainer instruction; 8-10 (Initial Public Push) requires manual go-ahead.
# story 8-8 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 4 MINOR; 2 fixed inline: 10d/e topics+description checks, 11d fallback section)+Code-Review CLEAN (2 MINOR maintainer-decision)+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-7 done (pipeline: ATDD+Dev+Test-Review (4 MAJOR + 4 MINOR fixed inline: yml struct validation for bug+feature, GitLab MR prohibition+sections, verify-script in emoji-array, -x bit check)+Code-Review (1 MINOR fixed: dependabot gomod /gateway+/media)+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-6 done (pipeline: ATDD+Dev+Test-Review (4 MAJOR + 4 MINOR fixed inline: verify-scripts all-3, partial-SHA detection, AC5 cache, AC7 triggers, /tmp→mktemp, if-no-files-found)+Code-Review (3 MINOR: ci-local --help, image-tag, permissions block)+Kassandra HIGH H-1 fixed (gitleaks SHA256 verification on both CIs) + M-2 cache narrowed + M-3 timeout/concurrency; M-1 ci-local.sh Docker hardening deferred): 2026-04-28
# story 8-5 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 4 MINOR fixed inline: F1 null-JSON false-pass, F2 runbook AC test added, F3 dead sandbox copies removed, F4 secretGroup=1)+Code-Review (1 MINOR fixed: test-count 8→9)+Kassandra CLEAN (2 MEDIUM, MEDIUM-2 pre-push hook bug fixed inline)): 2026-04-28
# story 8-4 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 2 MINOR fixed inline: F1 weak prohibition sub-assert, F3 Security tab link)+Code-Review CLEAN+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-3 done (pipeline: ATDD (via create-story)+Dev+Test-Review (0 MAJOR + 2 MINOR fixed inline: AC10 npx absolute-path false-negative)+Code-Review (1 MINOR + 2 INFO fixed inline: CoC contradiction, AC8b prohibition phrase, AC5b OpenAPI mention, AC4 anchored heading)+Security skipped (not-needed)): 2026-04-28
# story 8-2 done (pipeline: ATDD+Dev+Test-Review (1 MAJOR + 3 MINOR fixed inline: AC6 BSD-grep emoji false-positive, AC3/AC4 section-scoping, link-syntax)+Code-Review (tempfile trap added)+Security skipped (not-needed)): 2026-04-29
# story 8-1 done (pipeline: ATDD+Dev (1 fix-iteration: 3 MAJOR)+Test-Review CLEAN+Code-Review (5 MINOR fixed inline + F6 detached-HEAD added)+Kassandra CLEAN, force-with-lease in runbook): 2026-04-28
# story 8-11 done (pipeline: ATDD+Dev+Code CLEAN — WaitAndRunMigrations retry loop, wait-for-stack.sh, integration-test-k8s job): 2026-05-04
# story 8-10a done (pipeline: ATDD+Dev+Test-Review (2 MAJOR fixed inline: AC4b sync wakeup test + AC5b E2E-only note, 2 MINOR fixed: pg.leave on_exit + ollama unstage)+Code-Review CLEAN (1 MINOR inline: unrelated script unstaged)+Kassandra CLEAN (0 MEDIUM, 2 INFO)): 2026-04-29
# story 8-10a created (pre-release bug fixes: invite :pg notification + profile upsert on login): 2026-04-29
# strategic pivot (2026-04-28): dual-host publication — github.com/innoq/nebu (GitHub Actions) + gitlab.opencode.de/nebu/nebu-server (GitLab CI). 8-6 NOT skipped. 8-7 expanded to .github/ + .gitlab/ templates. 8-10 pushes to both remotes. Retro-fix for GitHub-only references in 8-1/8-3/8-4 deferred to dedicated chore commit before 8-10.
# story 8-9 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 3 MINOR; M-1 mode-field assertion + INFO-2 umlaut fixed inline)+Code-Review (3 MINOR markdownlint fixed inline)+Kassandra CLEAN (2 MEDIUM, M-1 --quick warning banner fixed inline)): 2026-04-28 — pipeline stops here per maintainer instruction; 8-10 (Initial Public Push) requires manual go-ahead.
# story 8-8 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 4 MINOR; 2 fixed inline: 10d/e topics+description checks, 11d fallback section)+Code-Review CLEAN (2 MINOR maintainer-decision)+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-7 done (pipeline: ATDD+Dev+Test-Review (4 MAJOR + 4 MINOR fixed inline: yml struct validation for bug+feature, GitLab MR prohibition+sections, verify-script in emoji-array, -x bit check)+Code-Review (1 MINOR fixed: dependabot gomod /gateway+/media)+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-6 done (pipeline: ATDD+Dev+Test-Review (4 MAJOR + 4 MINOR fixed inline: verify-scripts all-3, partial-SHA detection, AC5 cache, AC7 triggers, /tmp→mktemp, if-no-files-found)+Code-Review (3 MINOR: ci-local --help, image-tag, permissions block)+Kassandra HIGH H-1 fixed (gitleaks SHA256 verification on both CIs) + M-2 cache narrowed + M-3 timeout/concurrency; M-1 ci-local.sh Docker hardening deferred): 2026-04-28
# story 8-5 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 4 MINOR fixed inline: F1 null-JSON false-pass, F2 runbook AC test added, F3 dead sandbox copies removed, F4 secretGroup=1)+Code-Review (1 MINOR fixed: test-count 8→9)+Kassandra CLEAN (2 MEDIUM, MEDIUM-2 pre-push hook bug fixed inline)): 2026-04-28
# story 8-4 done (pipeline: ATDD+Dev+Test-Review (0 MAJOR + 2 MINOR fixed inline: F1 weak prohibition sub-assert, F3 Security tab link)+Code-Review CLEAN+Security skipped (optional → heuristic not-needed)): 2026-04-28
# story 8-3 done (pipeline: ATDD (via create-story)+Dev+Test-Review (0 MAJOR + 2 MINOR fixed inline: AC10 npx absolute-path false-negative)+Code-Review (1 MINOR + 2 INFO fixed inline: CoC contradiction, AC8b prohibition phrase, AC5b OpenAPI mention, AC4 anchored heading)+Security skipped (not-needed)): 2026-04-28
# story 8-2 done (pipeline: ATDD+Dev+Test-Review (1 MAJOR + 3 MINOR fixed inline: AC6 BSD-grep emoji false-positive, AC3/AC4 section-scoping, link-syntax)+Code-Review (tempfile trap added)+Security skipped (not-needed)): 2026-04-29
# story 8-1 done (pipeline: ATDD+Dev (1 fix-iteration: 3 MAJOR)+Test-Review CLEAN+Code-Review (5 MINOR fixed inline + F6 detached-HEAD added)+Kassandra CLEAN, force-with-lease in runbook): 2026-04-28
# story 6-11 created: 6-11 → ready-for-dev: 2026-05-01 (Gherkin: Admin API CRUD Flow — 3 Godog scenarios: user lifecycle deactivate/reactivate, role grant/revoke, room archive, new gateway/features/admin_api.feature + gateway/test/integration/admin_api_steps_test.go)
# story 6-9 created: 6-9 → ready-for-dev: 2026-05-01 (Room Archivierung — POST /admin/rooms/{roomId}/archive + unarchive, proto ArchiveRoom/UnarchiveRoom, Room.Server archived init guard, SendEvent archive check, audit log)
# story 6-8 created: 6-8 → ready-for-dev: 2026-05-01 (Room Settings Update API — PATCH /admin/rooms/{roomId}, PUT /admin/config/room-defaults, migration 000037 room_defaults, proto UpdateRoomSettings, max_members enforcement in Room GenServer, audit log)
# story 6-7 created: 6-7 → ready-for-dev: 2026-05-01 (Room List + Get API — migration 000036 rooms_admin_columns, GET /admin/rooms list+filter+cursor, GET /admin/rooms/{roomId} detail, RoomRepository, member_count/message_count, audit log)
# story 6-6 done (pipeline: ATDD+Dev+CodeReview+Kassandra CLEAN — role_overrides migration 000035, POST /roles grant/revoke, RequireRole DB-override, 60s TTL cache): 2026-05-01
# story 6-6 created: 6-6 → ready-for-dev: 2026-05-01 (User Role Assignment API — role_overrides migration 000035, POST /roles grant/revoke, RequireRole DB-override extension with 60s TTL cache, roles merge in ListUsers/GetUser)
# story 6-5 created: 6-5 → ready-for-dev: 2026-05-01 (User Deactivation + Reactivation + Session-Invalidierung — POST deactivate/reactivate, proto InvalidateUserSessions, JWT middleware is_active check, migration 000034)
# story 6-4 created: 6-4 → ready-for-dev: 2026-05-01 (User List + Get API — ListAdminUsers real impl + GetAdminUser new endpoint, spec-first with make gen-api, UserRepository, cursor pagination, email masking MVP, audit log)
# story 6-3 created: 6-3 → ready-for-dev: 2026-05-01 (Admin API Router + RequireRole middleware — wires oapi-codegen StrictHandler + role-gate for instance_admin/compliance_officer groups)
# story 6-5 done (pipeline: ATDD+Dev+CodeReview+Kassandra HIGH-1-fixed (jwtWithStatusCheck wraps ALL routes) — deactivate/reactivate, proto InvalidateUserSessions, 60s TTL cache): 2026-05-01
# story 6-4 done (pipeline: ATDD+Dev+CodeReview+Kassandra CLEAN — UserList/GetUser, cursor pagination, email-mask MVP, audit log, errors.Is fix): 2026-05-01
# story 6-3 done (pipeline: ATDD+Dev+CodeReview MAJOR-fixed (compliance route regression)+Kassandra CLEAN — RequireRole, RegisterAdminRoutes, compliance-route owned by main.go): 2026-05-01
# story 6-2 done (pipeline: ATDD+Dev+CodeReview CLEAN — APIResponse[T], EncodeCursor/DecodeCursor, ErrInvalidCursor sentinel): 2026-05-01
# story 6-2 created: 6-2 → ready-for-dev: 2026-05-01 (Admin API response envelope + cursor pagination helpers)
# story 6-1 done (pipeline: ATDD+Dev+CodeReview+Kassandra CLEAN — oapi-codegen StrictServerInterface, openapi.yaml 3.1, GET /api/v1/openapi.yaml embedded): 2026-05-01
# story 7-36 done (pipeline: ATDD+Dev+CodeReview CLEAN — sync propagation Godog + private room exclusion + theResponseBodyIs compile fix): 2026-04-30
# story 7-36 created: 7-36 → ready-for-dev: 2026-04-30 (P1 test gap closure: sync propagation for account_data+tags + private room exclusion from publicRooms)
# story 7-35 done (pipeline: ATDD+Dev+CodeReview MINOR-FIXED — withUserDB helper, SET LOCAL GUC, RLS migration 000033): 2026-04-30
# story 7-33 done (pipeline: ATDD+Dev+CodeReview CLEAN — system-role bypass in get_room_state, fanout goroutine fixed): 2026-04-30
# story 7-33 created: 7-33 → ready-for-dev: 2026-04-30 (SEC Gate 2 MEDIUM fix: fanout goroutine must send system role to bypass membership check in get_room_state)
# story 7-32 done (pipeline: ATDD+Dev+CodeReview CLEAN — kick/ban/unban use metadata not body caller_id): 2026-04-30
# story 7-32 created: 7-32 → ready-for-dev: 2026-04-30 (SEC Gate 2 HIGH fix: moderation caller_id must come from gRPC metadata not request body)
# epic-7b SEC Gate 2 done — Kassandra HIGH (kick/ban/unban caller_id from body not metadata); 6 MEDIUM; follow-ups needed: 2026-04-30
# story 7-30 done (pipeline: ATDD+Dev+CodeReview 1 MAJOR fixed (SELECT FOR UPDATE in PutRule) + 5 MINOR fixed): 2026-04-30
# story 7-29 done (pipeline: CodeReview 1 MAJOR fixed (RLS→WHERE clause) + 3 MINOR fixed (slog, matrixURL, captureResponse): 2026-04-30
# story 7-28 done (pipeline: CodeReview MINOR fixed (slog, auth guard panic) + compile errors fixed): 2026-04-30
# story 7-27 done (pipeline: CodeReview 2 MINOR fixed (gRPC ctx propagation, slog) + compile errors fixed): 2026-04-30
# story 7-26 done (pipeline: ATDD+Dev+CodeReview MAJOR accepted (token-invalidation MVP risk)+Security Kassandra CLEAN): 2026-04-30
# story 7-25 done (pipeline: ATDD+Dev+CodeReview CLEAN — tags read-modify-write via m.tag account data entry): 2026-04-30
# story 7-24 done (pipeline: ATDD+Dev+CodeReview CLEAN — account_data table migration 000029, upsert, sync integration): 2026-04-30
# story 7-23 done (pipeline: ATDD+Dev+CodeReview CLEAN — aliases stub returns [], JWT-gated): 2026-04-30
# story 7-22 done (pipeline: ATDD+Dev+CodeReview 5 MAJOR fixed (kick wrong-sender+ban double-event+reason dropped+forget-stub AC noted)+Security not-needed per CR): 2026-04-30
# story 7-21 done (pipeline: ATDD+Dev+CodeReview CLEAN — 1 MINOR (404 wording per spec accepted)): 2026-04-30
# story 7-20 done (pipeline: ATDD+Dev+CodeReview CLEAN — 2 MINOR fixed (Godog field assertion+comment drift), N+1 profile lookup acceptable for MVP): 2026-04-30
# story 7-19 done (pipeline: ATDD+Dev+CodeReview MAJOR fixed (IDOR membership check server.ex)+3 MINOR fixed (Gherkin keys+step parser+gofmt)): 2026-04-30
# story 7-18 done (pipeline: ATDD+Dev+CodeReview MINOR_FIXED (3 MAJOR Playwright+1 MINOR compliance)+Security CLEAN): 2026-04-30
# story 7-17 done (pipeline: ATDD+Dev+CodeReview CLEAN+Security CLEAN — 11 POST routes wrapped bodyLimit64KiB(csrf(sessionGuard))); Kassandra MEDIUM non-blocking): 2026-04-30
# story 7-16f done (pipeline: no-op — migration 000026_avatar_url_scrub already committed in feat(security) 4e25855; story was deferred AC7 from 5.29b, now verified in place): 2026-04-30
# story 7-16e done (pipeline: MINOR_FIXED+Security CLEAN — migration 000028 re-asserts audit_log_purge SECURITY DEFINER; test INSERT+UPDATE to bypass BEFORE INSERT trigger): 2026-04-30
# story 7-16d done (pipeline: CLEAN — missing 4 RPC registrations in core_grpc.pb.ex fixed; auth interceptor now runs for WriteAuditLog/GetPresence/UpdateProfile/DeleteUserKeys): 2026-04-30
# story 7-16c done (pipeline: ATDD+Dev+CodeReview MINOR_FIXED+Security CLEAN — gRPC pipeline smoke tests, logout HTTP flow, compile-collision fixed): 2026-04-30
# story 7-16b done (pipeline: ATDD+Dev+CodeReview+Security CLEAN — complianceRL isolated from strictRL login bucket): 2026-04-30
# story 7-16a done (pipeline: test-only fix — migrationDBURL BYPASSRLS for TRUNCATE, security_review: not-needed): 2026-04-30
# story 7-16f created: 7-16f → ready-for-dev: 2026-04-30
# story 7-16e created: 7-16e → ready-for-dev: 2026-04-30
# story 7-16d created: 7-16d → ready-for-dev: 2026-04-30
# story 7-16c created: 7-16c → ready-for-dev: 2026-04-30
# story 7-16b created: 7-16b → ready-for-dev: 2026-04-30
# story 7-16a created: 7-16a → ready-for-dev: 2026-04-30 (bugfix batch: integration-test regressions + missing features post-epic-7-rebase)
# story 7-6 done (CR: 2 MINOR fixed inline — rune-count fix + dead template branch removed; 0 decision-needed, 0 deferred): 2026-04-29
# story 7-6 created: 7-6 → ready-for-dev: 2026-04-29
# story 7-5 done (CR: 1 MINOR fixed inline — badge normalization extracted to toUserRowData helper; 0 decision-needed, 0 deferred): 2026-04-29
# story 7-5 created: 7-5 → ready-for-dev: 2026-04-29
# story 7-4 created: 7-4 → ready-for-dev: 2026-04-29
# story 7-2 done (pipeline: 3 TEA MAJOR fixed inline + 2 MINOR fixed, CR 1 MINOR fixed — not-found title casing; tokens: ~304k create+dev+tea+cr): 2026-04-29
# story 7-1 done (pipeline: 2 TEA MINOR fixed inline, CR CLEAN; tokens: ~267k create+dev+tea+cr): 2026-04-29
# story 7-2 review: 2026-04-29
# story 7-2 ready-for-dev: 2026-04-29
# story 5-29d review (AC1: event_dispatcher 23→8 failures, remaining 8 split to 5-29d.1; AC5 KEK hard-fail; AC6 enc:v1: envelope; AC7 jitter scheduler; AC3 deferred to 7-11): 2026-04-23
# story 5-29d in-progress: 2026-04-23
# story 5-29b done (pipeline: 0 TEA MAJOR / 7 MINOR fixed by CR; Kassandra CLEAN — URL-encoded path-traversal verified non-exploitable, 4 INFO no-blocker): 2026-04-29
# story 5-29c done (pipeline: 2 TEA MAJOR (event_time trigger seed-bypass + plaintext-key migration) + 2 MINOR fixed inline; Kassandra HIGH-1 (CSRF on revoke endpoint) fixed inline; FB-29c-1..4 deferred to 5-29d): 2026-04-29
# story 5-29e done (pipeline: 0 TEA MAJOR / 4 MINOR fixed by CR; +1 NEW bug 4 (Element /sync polling loop via missing device fields, tmp/snyc-bug.md) fixed inline; Kassandra skipped (security_review: optional)): 2026-04-23
# story 5-29a done (pipeline: 0 TEA MAJOR / 4 MINOR fixed by CR + 1 self-discovered MAJOR (function-ownership transfer) fixed; Kassandra HIGH-1 (silent purge under FORCE RLS) fixed inline via BYPASSRLS for nebu_migrate): 2026-04-23
# story 5-29 split into 5-29a..e on user request (23 deferred items + 3 manual-testing bugs from tmp/test-findings.md): 2026-04-23
# story 5-28 done (meta-story: pipeline gate already implemented in skills/bmad-pipeline + CLAUDE.md, retroactively validated by 10+ stories in this run): 2026-04-23
# epic 5 SEC Gate 2 (Kassandra epic-wide review 267a4bf..HEAD, 189 files): HIGH non-blocking, 6 new findings appended to 5-29 (FB-E5-04..09): 2026-04-23
# story 5-9 done (pipeline: test-only Gherkin scenarios, 2 TEA MAJOR + 1 MINOR fixed inline (audit_log columns + RLS structural-vs-behavioural pg_policies check + target_id filter), Kassandra skipped (security_review: optional)): 2026-04-23
# story 5-9 created: 5-9 → ready-for-dev: 2026-04-23
# story 5-8 done (pipeline: 2 TEA MAJOR fixed inline (production helper + vacuous query-capture), 1 CR MAJOR (path-traversal arbitrary-file-delete) + 1 MINOR fixed, Kassandra CLEAN; FB-58-01/02/03 deferred to 5-29): 2026-04-23
# story 5-8 created: 5-8 → ready-for-dev: 2026-04-23
# story 5-7 done (pipeline: dev committed early aa1cc89, 0 TEA MAJOR / 4 MINOR + 1 audit-shadowing bug fixed in CR 7180486, Kassandra CLEAN; FB-57-01 MEDIUM (TOCTOU on deletion_status) deferred to 5-29): 2026-04-23
# story 5-7 created: 5-7 → ready-for-dev: 2026-04-23
# story 5-6 done (pipeline: 0 TEA MAJOR / 3 MINOR + DoS LIMIT-guard fixed by CR, Kassandra CLEAN; FB-56-01 MEDIUM (status TOCTOU + streaming) deferred to 5-29): 2026-04-23
# story 5-6 created: 5-6 → ready-for-dev: 2026-04-23
# story 5-5 done (pipeline: 3 TEA MAJOR + 2 MINOR fixed in-pipeline, 1 CR MAJOR (DO NOTHING+re-read race) + 1 MINOR fixed, Kassandra CLEAN; 1 functional bug (set_at NOT NULL) + iat-future-check fixed inline; FB-55-01 MEDIUM deferred to 5-29): 2026-04-23
# story 5-5 created: 5-5 → ready-for-dev: 2026-04-23
# story 5-4 done (pipeline: 0 TEA MAJOR / 5 MINOR + 2 self-found MINOR fixed by CR, Kassandra CLEAN; FB-54-01 LOW deferred to 5-29): 2026-04-23
# story 5-4 created: 5-4 → ready-for-dev: 2026-04-23
# story 5-3 done (pipeline: 0 TEA MAJOR / 5 MINOR fixed by CR, Kassandra CLEAN; FB-53-01 MEDIUM + FB-53-02/03 LOW deferred to 5-29): 2026-04-23
# story 5-3 created: 5-3 → ready-for-dev: 2026-04-23
# story 5-2 done (pipeline: 4 TEA MAJOR fixed in-pipeline, 2 CR MINOR self-fixed, Kassandra HIGH non-blocking; FB-52-01 HIGH + FB-52-02 MEDIUM + FB-E5-03 MEDIUM deferred to 5-29): 2026-04-23
# epic 8 draft added to planning (separate commit, not tied to 5-2 scope): 2026-04-23
# story 5-2 created: 5-2 → ready-for-dev: 2026-04-23
# story 5-1 done (pipeline: 2 TEA MAJOR fixed in-pipeline, 2 security MAJOR self-fixed by CR, Kassandra CLEAN; FB-51-01 HIGH + FB-51-02 MEDIUM/LOW deferred to 5-29): 2026-04-23
# story 5-25 in-progress: 2026-04-23
# story 5-24 review: 2026-04-23
# story 5-24 in-progress: 2026-04-23
# story 4-24 review: 2026-04-11
# story 4-24 in-progress: 2026-04-11
# story 4-24 created: 4-24 → ready-for-dev: 2026-04-11
# story 4-23 review: 2026-04-03
# story 4-23 in-progress: 2026-04-03
# story 4-23 created: 4-23 → ready-for-dev: 2026-04-03
# story_location: _bmad-output/implementation-artifacts
# story 6-10 created: 6-10 → ready-for-dev: 2026-05-01 (Server Config API + Metrics API — GET/PATCH /admin/config, GET /admin/metrics, proto InvalidateAllAdminSessions, ServerConfigRepository, MetricsRepository, oidc_client_secret encryption, session invalidation on OIDC change)
# story 6-1 created: epic-6 → in-progress, 6-1 → ready-for-dev: 2026-05-01
# story 5-1 review: 2026-04-23 (audit-log schema + RLS + retention cleanup)
# story 5-29 created as follow-up of 5-27 scope reduction (code-review MAJOR-B): 2026-04-23
# story 5-27 done (pipeline: 1 MAJOR-A fixed, 1 MAJOR-B deferred to 5-29, 8 MINOR fixed, Kassandra CLEAN): 2026-04-23
# story 5-26 done (pipeline: 2 MAJOR fixed (TEA), 1 MINOR fixed (CR) — LIKE escape + input validation): 2026-04-23
# story 5-25 done (pipeline: ATDD+Dev+Code+Security CLEAN — loginToken TTL 5m→30s): 2026-04-23
# story 5-24 done (pipeline: ATDD+Dev+Code+Security CLEAN — SSO redirect scheme allowlist): 2026-04-23
# story 5-23 done (pipeline: security fix — denylist check after signature verification): 2026-04-23
# story 5-22 done (pipeline: ATDD+Dev+Code+Security review CLEAN): 2026-04-22
# story 5-21 done (pipeline: 2 MAJOR + HIGH fixed, 2 rounds Kassandra): 2026-04-22
# story 5-20 done (pipeline: 1 MINOR fixed — 11 missing endpoints): 2026-04-22
# story 5-19 done (pipeline: ATDD+Dev in one pass, CLEAN): 2026-04-22
# story 5-18 done (pipeline: CLEAN, algs parsed once at construction): 2026-04-22
# story 5-17 done (pipeline: 2 MAJOR + HIGH fixed, shared validate pkg): 2026-04-22
# story 5-16 done (pipeline: 1 MAJOR fixed — AC5 empty-nonce bypass + legacy LoginHandler): 2026-04-22
# story 5-15 done (pipeline: 1 MAJOR fixed — redirect_uri scheme + HSTS XFF guard): 2026-04-22
# story 5-14 done (pipeline: 2 MINOR fixed — handler alloc + base.html inline style): 2026-04-22
# story 5-13 done (pipeline: 2 MINOR fixed — GET logout 405 + Playwright precondition): 2026-04-22
# story 5-12 done (pipeline: 2 MAJOR fixed — missing test + main.go wiring, HIGH gefixt): 2026-04-22
# story 5-11 done (pipeline: 3 rounds — real sql.Tx via runInTx injection): 2026-04-22
# story 5-10 done (pipeline: CLEAN, Bootstrap replay entry points closed): 2026-04-22
# story 4-29 done (code review passed, 2 MINOR fixed): 2026-04-15
# story 4-29 created: 4-29 → ready-for-dev: 2026-04-15
# story 4-22 created: 4-22 → ready-for-dev: 2026-04-03
# story 4-21 review: 2026-04-03
# story 4-21 in-progress: 2026-04-03
# story 4-21 created: 4-21 → ready-for-dev: 2026-04-03
# story 4-20 created: 4-20 → ready-for-dev: 2026-04-03
# story 4-19 created: 4-19 → ready-for-dev: 2026-04-09
# story 4-18 created: 4-18 → ready-for-dev: 2026-04-03
# story 4-17 created: 4-17 → ready-for-dev: 2026-04-03
# story 4-16 created: 4-16 → ready-for-dev: 2026-04-07
# story 4-15 created: 4-15 → ready-for-dev: 2026-04-07
# story 4-14 created: 4-14 → ready-for-dev: 2026-04-07
# story 4-13 created: 4-13 → ready-for-dev: 2026-04-03
# story 4-12 review: 2026-04-03
# story 4-12 in-progress: 2026-04-03
# story 4-12 created: 4-12 → ready-for-dev: 2026-04-03
# story 4-11 created: 4-11 → ready-for-dev: 2026-04-03
# story 4-10 created: 4-10 → ready-for-dev: 2026-04-03
# story 4-9 created: 4-9 → ready-for-dev: 2026-04-03
# story 4-8 created: 4-8 → ready-for-dev: 2026-04-03
# story 4-7 created: 4-7 → ready-for-dev: 2026-04-03
# story 4-6 created: 4-6 → ready-for-dev: 2026-04-03
# story 4-5 created: 4-5 → ready-for-dev: 2026-04-03
# story 4-4 done (code review passed, 2 MAJOR + 4 MINOR fixed): 2026-04-03
# story 4-4 review: 2026-04-03
# story 4-4 in-progress: 2026-04-03
# story 4-4 created: 4-4 → ready-for-dev: 2026-04-03
# story 4-3 review-fix (MAJOR: Map.new sort order, 2 MINOR): 2026-04-03
# story 4-3 review: 2026-04-03
# story 4-3 in-progress: 2026-04-03
# story 4-3 created: 4-3 → ready-for-dev: 2026-04-03
# story 4-2 done (code review passed, 1 MAJOR + 2 MINOR fixed): 2026-04-03
# story 4-2 review: 2026-04-03
# story 4-2 in-progress: 2026-04-03
# story 4-2 created: 4-2 → ready-for-dev: 2026-04-03
# story 4-1 done (code review passed, 0 MAJOR + 1 MINOR fixed): 2026-04-03
# story 4-1 review: 2026-04-03
# story 4-1 in-progress: 2026-04-02
# story 4-1 created: epic-4 → in-progress, 4-1 → ready-for-dev: 2026-04-02
# story 3-9 done (code review passed, 2 MAJOR + 3 MINOR fixes applied): 2026-04-02
# story 3-15 done (code review passed, 1 MINOR fix applied): 2026-04-01
# story 3-15 review: 2026-04-01
# story 3-15 in-progress: 2026-04-01
# story 3-15 created: 3-15 → ready-for-dev: 2026-04-01
# story 3-14 done (code review passed, 2 MINOR fixes applied): 2026-04-01
# story 3-14 review: 2026-04-01
# story 3-14 in-progress: 2026-04-01
# story 3-14 created: 3-14 → ready-for-dev: 2026-04-01
# story 3-13 done (code review passed, 1 MAJOR fix applied): 2026-04-01
# story 3-13 review: 2026-04-01
# story 3-13 in-progress: 2026-04-01
# story 3-13 created: 3-13 → ready-for-dev: 2026-04-01
# story 3-12 done (code review passed, 2 MINOR fixes applied): 2026-04-01
# story 3-12 review: 2026-04-01
# story 3-12 in-progress: 2026-04-01
# story 3-12 created: 3-12 → ready-for-dev: 2026-04-01
# story 3-11 done (code review passed): 2026-04-01
# story 3-11 created: 3-11 → ready-for-dev: 2026-04-01
# story 3-10 review: 2026-04-01
# story 3-10 in-progress: 2026-04-01
# story 3-10 created: 3-10 → ready-for-dev: 2026-04-01
# story 3-9 review: 2026-04-01
# story 3-9 in-progress: 2026-04-01
# story 3-9 created: 3-9 → ready-for-dev: 2026-04-01
# story 3-8 done (code review round 3 post-refactor passed): 2026-04-01
# story 3-8 review (refactored sync.Map → PostgreSQL draft storage): 2026-04-01
# story 3-8 done (code review round 2 passed): 2026-03-31
# story 3-8 in-progress → review (3 MAJOR fixes applied): 2026-03-31
# story 3-8 review → in-progress (3 MAJOR action items from code review): 2026-03-31
# story 3-8 review: 2026-03-31
# story 3-8 in-progress: 2026-03-31
# story 3-8 created: 3-8 → ready-for-dev: 2026-03-31
# story 3-7 done (code review passed): 2026-03-31
# story 3-7 review: 2026-03-31
# story 3-7 in-progress: 2026-03-31
# story 3-7 created: 3-7 → ready-for-dev: 2026-03-31
# story 3-6 done (code review passed): 2026-03-31
# story 3-6 review: 2026-03-31
# story 3-6 in-progress: 2026-03-31
# story 3-6 created: 3-6 → ready-for-dev: 2026-03-31
# story 3-5 done (code review passed): 2026-03-31
# story 3-5 review: 2026-03-31
# story 3-5 in-progress: 2026-03-31
# story 3-5 created: 3-5 → ready-for-dev: 2026-03-31
# story 3-4 done (code review passed): 2026-03-31
# story 3-4 review: 2026-03-31
# story 3-4 in-progress: 2026-03-31
# story 3-4 created: 3-4 → ready-for-dev: 2026-03-31
# story 3-3 done (code review passed): 2026-03-31
# story 3-3 review: 2026-03-31
# story 3-3 in-progress: 2026-03-31
# story 3-3 created: 3-3 → ready-for-dev: 2026-03-31
# story 3-2 done (code review passed): 2026-03-31
# story 3-2 review: 2026-03-31
# story 3-2 in-progress: 2026-03-31
# story 3-2 created: 3-2 → ready-for-dev: 2026-03-31
# story 3-1 done (code review passed): 2026-03-31
# story 3-1 review: 2026-03-31
# story 3-1 in-progress: 2026-03-31
# story 3-1 created: epic-3 → in-progress, 3-1 → ready-for-dev: 2026-03-31
# epic-2-retrospective done: 2026-03-31
# story 2-21 done (code review passed): 2026-03-31
# story 2-21 review: 2026-03-31
# story 2-21 in-progress: 2026-03-30
# story 2-21 created: 2-21 → ready-for-dev: 2026-03-30
# story 2-20 done (code review passed): 2026-03-30
# story 2-20 review: 2026-03-30
# story 2-20 in-progress: 2026-03-30
# story 2-20 created: 2-20 → ready-for-dev: 2026-03-30
# story 2-19 done (code review passed): 2026-03-30
# story 2-19 review: 2026-03-30
# story 2-19 in-progress: 2026-03-30
# story 2-19 created: 2-19 → ready-for-dev: 2026-03-30
# story 2-18 done (code review passed): 2026-03-30
# story 2-18 review: 2026-03-30
# story 2-18 in-progress: 2026-03-30
# story 2-18 created: 2-18 → ready-for-dev: 2026-03-30
# story 2-17 done (code review passed): 2026-03-30
# story 2-17 review: 2026-03-30
# story 2-17 in-progress: 2026-03-30
# story 2-17 created: 2-17 → ready-for-dev: 2026-03-30
# story 2-13 done (code review passed): 2026-03-30
# story 2-16 done (code review passed): 2026-03-30
# story 2-16 review: 2026-03-30
# story 2-16 in-progress: 2026-03-30
# story 2-16 created: 2-16 → ready-for-dev: 2026-03-30
# story 2-15 done (code review passed): 2026-03-30
# story 2-15 review: 2026-03-30
# story 2-15 in-progress: 2026-03-30
# story 2-15 created: 2-15 → ready-for-dev: 2026-03-30
# story 2-14 done (code review passed): 2026-03-30
# story 2-14 review: 2026-03-30
# story 2-14 in-progress: 2026-03-30
# story 2-14 created: 2-14 → ready-for-dev: 2026-03-30
# story 2-13 review: 2026-03-29
# story 2-13 created: 2-13 → ready-for-dev: 2026-03-29
# story 2-12 done (code review passed): 2026-03-27
# story 2-12 created: 2-12 → ready-for-dev: 2026-03-27
# story 2-11 done (code review passed): 2026-03-27
# story 2-11 review: 2026-03-27
# story 2-11 in-progress: 2026-03-27
# story 2-11 created: 2-11 → ready-for-dev: 2026-03-27
# story 2-10 done (code review passed): 2026-03-27
# story 2-10 review: 2026-03-27
# story 2-10 in-progress: 2026-03-27
# story 2-10 created: 2-10 → ready-for-dev: 2026-03-27
# story 2-9 done (code review passed): 2026-03-27
# story 2-9 review: 2026-03-27
# story 2-9 in-progress: 2026-03-27
# story 2-9 created: 2-9 → ready-for-dev: 2026-03-27
# story 2-8 done (code review passed): 2026-03-27
# story 2-8 review: 2026-03-27
# story 2-8 in-progress: 2026-03-27
# story 2-8 created: 2-8 → ready-for-dev: 2026-03-27
# story 2-7 done (code review passed): 2026-03-27
# story 2-7 review: 2026-03-27
# story 2-7 in-progress: 2026-03-27
# story 2-7 created: 2-7 → ready-for-dev: 2026-03-27
# story 2-6 done (code review passed): 2026-03-27
# story 2-6 created: 2-6 → ready-for-dev: 2026-03-27
# story 2-5 done (code review passed): 2026-03-27
# story 2-5 review: 2026-03-27
# story 2-5 in-progress: 2026-03-27
# story 2-5 created: 2-5 → ready-for-dev: 2026-03-27
# story 2-4 done (code review passed): 2026-03-27
# story 2-4 created: 2-4 → ready-for-dev: 2026-03-26
# story 2-3 done (code review passed): 2026-03-26
# story 2-3 review: 2026-03-26
# story 2-3 in-progress: 2026-03-26
# story 2-3 created: 2-3 → ready-for-dev: 2026-03-26
# story 2-2 done (code review passed): 2026-03-26
# story 2-2 review: 2026-03-26
# story 2-2 in-progress: 2026-03-26
# story 2-2 created: 2-2 → ready-for-dev: 2026-03-26
# story 2-1 done (code review passed): 2026-03-26
# story 2-1 review: 2026-03-26
# story 2-1 in-progress: 2026-03-26
# story 2-1 created: epic-2 → in-progress, 2-1 → ready-for-dev: 2026-03-26
# epic-1-retrospective done: 2026-03-26
# story 1-19 done (code review passed): 2026-03-26
# story 1-19 review: 2026-03-26
# story 1-19 in-progress: 2026-03-26
# story 1-19 created: 1-19 → ready-for-dev: 2026-03-25
# story 1-18 done (code review passed): 2026-03-25
# story 1-18 review: 2026-03-25
# story 1-18 in-progress: 2026-03-25
# story 1-18 created: 1-18 → ready-for-dev: 2026-03-25
# story 1-17 done (code review passed): 2026-03-25
# story 1-17 review: 2026-03-25
# story 1-17 in-progress: 2026-03-25
# story 1-17 created: 1-17 → ready-for-dev: 2026-03-25
# story 1-16 done (code review passed): 2026-03-25
# story 1-16 review: 2026-03-25
# story 1-16 in-progress: 2026-03-25
# story 1-16 created: 1-16 → ready-for-dev: 2026-03-25
# story 1-15 done (code review passed): 2026-03-25
# story 1-15 review: 2026-03-25
# story 1-15 in-progress: 2026-03-25
# story 1-15 created: 1-15 → ready-for-dev: 2026-03-25
# story 1-14 done (code review passed): 2026-03-25
# story 1-14 review: 2026-03-24
# story 1-14 in-progress: 2026-03-24
# story 1-14 created: 1-14 → ready-for-dev: 2026-03-24
# story 1-13 done (code review passed): 2026-03-24
# story 1-13 review: 2026-03-24
# story 1-13 in-progress: 2026-03-24
# story 1-13 created: 1-13 → ready-for-dev: 2026-03-24
# story 1-12 done (code review passed): 2026-03-24
# story 1-12 review: 2026-03-24
# story 1-12 in-progress: 2026-03-24
# story 1-12 created: 1-12 → ready-for-dev: 2026-03-24
# story 1-11 done (code review passed): 2026-03-24
# story 1-11 review: 2026-03-24
# story 1-11 in-progress: 2026-03-23
# story 1-11 created: 1-11 → ready-for-dev: 2026-03-23
# story 1-10 done (code review passed): 2026-03-23
# story 1-10 review: 2026-03-23
# story 1-10 in-progress: 2026-03-23
# story 1-10 created: 1-10 → ready-for-dev: 2026-03-23
# story 1-9 done (code review passed): 2026-03-23
# story 1-9 review: 2026-03-23
# story 1-9 in-progress: 2026-03-23
# story 1-9 created: 1-9 → ready-for-dev: 2026-03-23
# story 1-8 done (code review passed): 2026-03-23
# story 1-8 review: 2026-03-23
# story 1-8 created: 1-8 → ready-for-dev: 2026-03-23
# story 1-7 done (code review passed): 2026-03-23
# story 1-7 review: 2026-03-23
# story 1-7 created: 1-7 → ready-for-dev: 2026-03-23
# story 1-6 done (code review passed): 2026-03-23
# story 1-6 review: 2026-03-23
# story 1-6 in-progress: 2026-03-23
# story 1-6 created: 1-6 → ready-for-dev: 2026-03-23
# story 1-5 done (code review passed): 2026-03-20
# story 1-5 review: 2026-03-20
# story 1-5 in-progress: 2026-03-20
# story 1-5 created: 1-5 → ready-for-dev: 2026-03-20
# story 1-4 done (code review passed): 2026-03-20
# story 1-4 review: 2026-03-20
# story 1-4 in-progress: 2026-03-20
# story 1-4 created: 1-4 → ready-for-dev: 2026-03-20
# story 1-3 done (code review passed): 2026-03-20
# story 1-3 review: 2026-03-20
# story 1-3 in-progress: 2026-03-20
# story 1-3 created: 1-3 → ready-for-dev: 2026-03-20
# story 1-2 done (code review passed): 2026-03-20
# story 1-2 review: 2026-03-20
# story 1-2 in-progress: 2026-03-20
# story 1-2 created: 1-2 → ready-for-dev: 2026-03-20
# story 1-1 done (code review passed): 2026-03-20
# story 1-1 created: epic-1 → in-progress, 1-1 → ready-for-dev
# story 1-1 in-progress: 2026-03-20
# story 1-1 review: 2026-03-20
