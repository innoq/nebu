# E2E Test-Suite: Nebu

## Smoke-Test-Summary

| Test | File | Prio | Duration |
|------|------|------|----------|
| Bootstrap Layout | `admin/bootstrap.spec.ts` | P0 | ~10s |
| Bootstrap Happy Path | `admin/bootstrap-happy-path.spec.ts` | P0 | ~30s |
| Bootstrap Current State | `admin/bootstrap-current.spec.ts` | P0 | ~15s |
| SSO Login | `login/sso-login.spec.ts` | P0 | ~20s |
| Messages Send/Receive | `messages/messages.spec.ts` | P0 | ~15s |
| Room Lifecycle | `room/room-lifecycle.spec.ts` | P0 | ~20s |
| DM Creation (Bug 5.29e) | `dm/dm_create_bug_5_29e.spec.ts` | P0 | ~15s |
| Smoke Flows | `admin/smoke-flows.spec.ts` | P0 | ~20s |
| **Full-Stack Acceptance** | `acceptance/full-stack-acceptance.spec.ts` | P0 | ~2min |

### Lokaler Run

```bash
# Single smoke test (headed for debugging)
npx playwright test e2e/tests/features/acceptance --headed

# All E2E tests (excluding smoke tag for quick feedback)
npx playwright test e2e/tests --grep-invert "@smoke"

# All E2E tests including smoke
npx playwright test e2e/tests
```

### DB Reset

```bash
# Reset to pre-bootstrap state
docker compose exec -T postgres psql -U nebu -d nebu \
   -c "TRUNCATE TABLE server_config; TRUNCATE TABLE bootstrap_draft;"

# Full reset (caution: wipes all data)
docker compose exec -T postgres psql -U nebu -d nebu \
   -c "TRUNCATE TABLE server_config, bootstrap_draft, audit_log RESTART IDENTITY;"
```

### Full Test Inventory

| File | Type | Coverage |
|------|------|----------|
| `admin/bootstrap.spec.ts` | Playwright | Bootstrap wizard layout + basic flow |
| `admin/bootstrap-current.spec.ts` | Playwright | Current bootstrap state checks |
| `admin/bootstrap-happy-path.spec.ts` | Playwright | Full wizard + OIDC login E2E |
| `admin/smoke-flows.spec.ts` | Playwright | Admin deactivate user + archive room |
| `admin/users-page.spec.ts` | Playwright | Admin user list page |
| `admin/user-detail.spec.ts` | Playwright | Admin user detail page |
| `admin/user-role.spec.ts` | Playwright | Admin user role assignment |
| `admin/rooms-page.spec.ts` | Playwright | Admin room list page |
| `admin/room-detail.spec.ts` | Playwright | Admin room detail page |
| `admin/config.spec.ts` | Playwright | Admin config page |
| `admin/role-mapping.spec.ts` | Playwright | OIDC role mapping UI |
| `admin/audit-log.spec.ts` | Playwright | Audit log UI |
| `admin/compliance.spec.ts` | Playwright | Compliance UI |
| `admin/display-components.spec.ts` | Playwright | Display components |
| `admin/interaction-components.spec.ts` | Playwright | Interaction components |
| `admin/master-detail.spec.ts` | Playwright | Master-detail pattern |
| `admin/obsidian-theme.spec.ts` | Playwright | Dark theme (Obsidian) |
| `login/sso-login.spec.ts` | Playwright | SSO login via Element Web |
| `room/room-lifecycle.spec.ts` | Playwright | Room create/join/leave |
| `room/invites.spec.ts` | Playwright | Room invites |
| `messages/messages.spec.ts` | Playwright | Send/receive messages |
| `dm/dm_create_bug_5_29e.spec.ts` | Playwright | DM creation (bug fix 5.29e) |
| **`acceptance/full-stack-acceptance.spec.ts`** | **Playwright** | **Full-stack happy path (Story 8-11)** |

### CI Gate Integration

```yaml
test-acceptance:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - name: Start stack
      run: make dev
    - name: Wait for stack
      run: |
        for i in $(seq 1 30); do
          curl -sf http://localhost:8008/_matrix/client/versions && break
          sleep 2
        done
    - name: Install Playwright
      run: cd e2e && npm ci && npx playwright install --with-deps
    - name: Run Full-Stack Acceptance Test
      run: cd e2e && npx playwright test e2e/tests/features/acceptance --reporter=list
    - name: Run All E2E Regression
      run: cd e2e && npx playwright test e2e/tests --grep-invert "@smoke" --reporter=list
    - name: Upload screenshots on failure
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: e2e-screenshots
        path: e2e/test-results/
```

**Gate:** `make test-acceptance` muss grün sein **BEVOR** Story 8.10 (Initial Public Push) ausgeführt wird.
