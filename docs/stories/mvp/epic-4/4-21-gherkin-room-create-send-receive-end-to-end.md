# Story 4.21: Gherkin: Room Create + Send + Receive (End-to-End)

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-21-gherkin-room-create-send-receive-end-to-end
**Created:** 2026-04-03

---

## Story

As a developer,
I want Gherkin acceptance tests covering the full chat flow from room creation to message receipt,
so that regressions in the core messaging path are caught automatically in CI.

---

## Acceptance Criteria

1. `gateway/features/room_flow.feature` contains **Scenario: User creates a room, sends a message, and another user receives it**
   - Given two authenticated users `kai` (instance_admin) and `alex` (user) with OIDC tokens from Dex
   - When `kai` calls `POST /_matrix/client/v3/createRoom` with `name: "test-room"`
   - Then response is `200` with a `room_id`
   - When `kai` calls `POST /_matrix/client/v3/rooms/{room_id}/invite` with `alex`'s `user_id`
   - And `alex` calls `POST /_matrix/client/v3/join/{room_id}`
   - And `kai` calls `PUT /_matrix/client/v3/rooms/{room_id}/send/m.room.message/{txnId}` with body `{"msgtype":"m.text","body":"hello"}`
   - Then response is `200` with an `event_id`
   - When `alex` calls `GET /_matrix/client/v3/rooms/{room_id}/messages`
   - Then the response is `200` and the body contains `"hello"`

2. `gateway/features/room_flow.feature` contains **Scenario: txnId idempotency**
   - Given `kai` is authenticated and in a room
   - When `kai` sends the same `txnId` twice (PUT /send/m.room.message/{same_txnId})
   - Then both responses return `200` with the **identical** `event_id`
   - And `GET /rooms/{room_id}/messages` contains the message body exactly once

3. All new step definitions are implemented in `gateway/test/integration/room_flow_steps_test.go`

4. `InitializeScenario` in `gateway/test/integration/steps_test.go` calls `initializeRoomFlowSteps(sc)`

5. Both scenarios pass when `make test-integration` runs against the full stack

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Full room flow: create → invite → join → send → messages — Godog**
- Given: `kai` obtains Dex token (authorization code flow, same as auth.feature); `alex` obtains Dex token
- When: `kai` creates room → `kai` invites `alex` → `alex` joins → `kai` sends message "hello"
- Then: `GET /rooms/{room_id}/messages` as `alex` returns `200` with body containing `"hello"`

**2. txnId idempotency — Godog**
- Given: `kai` is authenticated and in a room (scenario can reuse the room created in the previous scenario if run in order, OR create a new room)
- When: `kai` PUT /send/m.room.message/{txnId} twice with the same `txnId` and same body
- Then: both responses return `200` with identical `event_id` values
- And: `GET /rooms/{room_id}/messages` body contains `"hello"` exactly once (no duplicate message in timeline)

---

## Tasks / Subtasks

- [x] Task 1: Create `gateway/features/room_flow.feature` (AC: 1, 2)
  - [x] 1.1 Write Feature header
  - [x] 1.2 Write Scenario: full room create → invite → join → send → messages flow
  - [x] 1.3 Write Scenario: txnId idempotency (send same txnId twice)

- [x] Task 2: Create `gateway/test/integration/room_flow_steps_test.go` (AC: 3)
  - [x] 2.1 Add `//go:build integration` tag, `package integration_test`
  - [x] 2.2 Add package-level state vars: `lastRoomID`, `lastEventID`, `kaiAccessToken`, `alexAccessToken`, `kaiUserID`, `alexUserID`, `lastTxnID`, `lastSecondEventID`
  - [x] 2.3 Implement `kaiIsAuthenticated()` — Dex auth code flow for `kai@example.com` / `changeme`, then POST /login, store `kaiAccessToken` and `kaiUserID`
  - [x] 2.4 Implement `alexIsAuthenticated()` — Dex auth code flow for `alex@example.com` / `changeme`, then POST /login, store `alexAccessToken` and `alexUserID`
  - [x] 2.5 Implement `kaiCreatesARoom(name string)` — POST `/createRoom`, store `lastRoomID`
  - [x] 2.6 Implement `kaiInvitesAlex()` — POST `/rooms/{lastRoomID}/invite` with `alexUserID`
  - [x] 2.7 Implement `alexJoinsTheRoom()` — POST `/join/{lastRoomID}` as `alex`
  - [x] 2.8 Implement `kaiSendsMessage(body string)` — PUT `/rooms/{lastRoomID}/send/m.room.message/{txnId}`, store `lastEventID`; generate `txnId` as `fmt.Sprintf("txn-%d", time.Now().UnixNano())`
  - [x] 2.9 Implement `alexRetrievesMessagesFromTheRoom()` — GET `/rooms/{lastRoomID}/messages` as `alex`, store `lastStatusCode` and `lastBody`
  - [x] 2.10 Implement `kaiSendsTheSameMessageAgain()` — PUT same txnId as before, store response as `lastSecondEventID`
  - [x] 2.11 Implement `bothSendsReturnedTheSameEventID()` — assert `lastEventID == lastSecondEventID`
  - [x] 2.12 Implement `theBodyContainsExactlyOnce(substr string)` — count occurrences of `substr` in `lastBody`, assert == 1
  - [x] 2.13 Expose `initializeRoomFlowSteps(sc *godog.ScenarioContext)` registering all steps

- [x] Task 3: Update `gateway/test/integration/steps_test.go` (AC: 4)
  - [x] 3.1 Add `initializeRoomFlowSteps(sc)` call at end of `InitializeScenario`

- [ ] Task 4: Verify `make test-integration` passes (AC: 5)
  - [ ] 4.1 `docker compose up -d --wait` — all services healthy
  - [ ] 4.2 Confirm godog reports all scenarios (health + auth + admin + room_flow), all steps pass, exit 0

---

## Dev Notes

### Current State — What Exists

| File | State | Action |
|------|-------|--------|
| `gateway/features/health.feature` | EXISTS, passing | Do NOT modify |
| `gateway/features/auth.feature` | EXISTS, passing | Do NOT modify |
| `gateway/features/admin_bootstrap.feature` | EXISTS, passing | Do NOT modify |
| `gateway/features/room_flow.feature` | MISSING | CREATE |
| `gateway/test/integration/main_test.go` | EXISTS — all URL vars initialized | Do NOT modify |
| `gateway/test/integration/steps_test.go` | EXISTS — calls `initializeAuthSteps`, `initializeAdminBootstrapSteps` | UPDATE — add `initializeRoomFlowSteps(sc)` call only |
| `gateway/test/integration/auth_steps_test.go` | EXISTS | Do NOT modify |
| `gateway/test/integration/admin_bootstrap_steps_test.go` | EXISTS | Do NOT modify |
| `gateway/test/integration/room_flow_steps_test.go` | MISSING | CREATE |

**Do NOT touch:** `docker-compose.yml`, `Makefile`, any Go production files, any Elixir files, `dev/dex/config.yaml`, proto files.

### Critical: File Location for Feature Files

Feature files MUST be placed at `gateway/features/` (NOT `test/features/` or `tests/features/`). The epics.md mentions `tests/features/` but the actual running project uses `gateway/features/` (confirmed by `main_test.go`: `Paths: []string{"../../features"}`). The existing passing tests (`health.feature`, `auth.feature`, `admin_bootstrap.feature`) prove this location is correct.

### Critical: Test Network and URL Routing

Matrix Client-Server API runs on port **8008** (not 8080). Use `matrixURL` (already initialized in `main_test.go` from `NEBU_TEST_MATRIX_URL=http://gateway:8008`). All Matrix API endpoints go to `matrixURL`.

Available URL variables (from `main_test.go`):
- `gatewayURL` = `http://gateway:8080` (health, admin)
- `matrixURL` = `http://gateway:8008` (all `/_matrix/client/v3/...` calls)
- `dexURL` = `http://dex:5556` (Dex auth)
- `coreURL` = `http://core:4000`
- `dbURL` = PostgreSQL DSN
- `internalSecret` = HMAC secret

### Critical: State Variables Are Package-Level

All state variables (`lastStatusCode`, `lastBody`, `lastLocationHeader`, `lastDexIDToken`, `lastAccessToken`) in `steps_test.go` and `auth_steps_test.go` are package-level — they are shared across all step files in `package integration_test`. `room_flow_steps_test.go` can read and write `lastStatusCode` and `lastBody` directly. Declare new vars for room flow state in `room_flow_steps_test.go`:

```go
var lastRoomID string
var lastEventID string
var kaiAccessToken string
var alexAccessToken string
var kaiUserID string
var alexUserID string
var lastTxnID string
var lastSecondEventID string
```

### Critical: Dex Authorization Code Flow (Copy from auth_steps_test.go)

Dex v2.41 does NOT support `grant_type=password` (ROPC) in practice. The implementation in `auth_steps_test.go` uses a programmatic authorization code flow that:
1. GET `/dex/auth?response_type=code&client_id=nebu-gateway&redirect_uri=...&scope=openid+profile+email+groups&state=teststate` — follows redirects to the HTML form
2. Extracts `<form action="...">` from HTML (HTML-unescape `&amp;` → `&`)
3. POST form credentials (`login=username&password=password`) with a no-redirect client
4. Extracts `code` from the `Location` header redirect
5. POST `/dex/token` with `grant_type=authorization_code`, `code`, `redirect_uri`, `client_id=nebu-gateway`, `client_secret=nebu-dev-secret`
6. Extracts `id_token` from the JSON response

The `redirect_uri` MUST be exactly `"http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc"` (matches Dex `staticClients[0].redirectURIs`).

**Do NOT duplicate this function.** Instead, expose a helper or replicate the pattern with different usernames. Since `iObtainDexTokenFor` in `auth_steps_test.go` stores the result in `lastDexIDToken`, `room_flow_steps_test.go` needs its own version that stores into `kaiAccessToken`/`alexAccessToken` directly, or calls the existing function then reads `lastDexIDToken`.

**Recommended pattern**: Call `iObtainDexTokenFor(username, password)` from `auth_steps_test.go` (it is package-scoped, callable from `room_flow_steps_test.go`), then call `iPostLoginWithDexToken()` to get the Matrix `access_token`. Store `lastAccessToken` into the appropriate `kaiAccessToken` / `alexAccessToken` variable. Also parse `user_id` from the login response to populate `kaiUserID` / `alexUserID`.

### Critical: Matrix user_id Format

The Dex `sub` claim is protobuf-encoded (NOT the raw UUID). The `user_id` in the Matrix login response is formatted as `@<protobuf_encoded_sub>:<server_name>`.

From Story 2-21 dev notes, `kai`'s `user_id` login response contains `CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE` (the protobuf-encoded sub for UUID `00000000-0000-0000-0000-000000000001`).

For `alex` (UUID `00000000-0000-0000-0000-000000000003`): the protobuf-encoded sub will be similar. **Do not hardcode the sub** — parse `user_id` from the `POST /login` JSON response instead. The login response returns:

```json
{
  "access_token": "<jwt>",
  "device_id": "...",
  "user_id": "@CiQw...:localhost",
  "home_server": "localhost"
}
```

Parse and store this `user_id` for use in the invite step.

### Critical: Strict Mode = All Steps Must Be Registered

`main_test.go` has `Strict: true`. Any step in any `.feature` file that lacks a matching `sc.Step(...)` registration will cause the entire suite to fail with an undefined step error. Register ALL steps used in `room_flow.feature` in `initializeRoomFlowSteps`.

### Critical: Scenario Isolation — Clean State Per Scenario

Each Godog scenario runs sequentially. The room created in Scenario 1 (full flow) will still exist when Scenario 2 (idempotency) starts. Design Scenario 2 to either:
- Reuse `lastRoomID` from Scenario 1 (kai is already in the room, no re-create needed), OR
- Create its own room (simpler, fully self-contained)

**Recommended**: Scenario 2 creates its own room for full isolation. This avoids ordering dependencies between scenarios.

### Endpoint Reference — Matrix API (all on matrixURL port 8008)

| Method | Path | Auth | Handler |
|--------|------|------|---------|
| POST | `/_matrix/client/v3/createRoom` | Bearer | `CreateRoomHandler.PostCreateRoom` |
| POST | `/_matrix/client/v3/rooms/{roomId}/invite` | Bearer | `InviteUserHandler.PostInviteUser` |
| POST | `/_matrix/client/v3/join/{roomIdOrAlias}` | Bearer | `JoinRoomHandler.PostJoinRoom` |
| PUT | `/_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` | Bearer | `SendEventHandler.PutSendEvent` |
| GET | `/_matrix/client/v3/rooms/{roomId}/messages` | Bearer | `GetMessagesHandler.GetMessages` |

Request/response formats:

**POST /createRoom** request:
```json
{"name": "test-room"}
```
Response:
```json
{"room_id": "!<id>:localhost"}
```

**POST /rooms/{roomId}/invite** request:
```json
{"user_id": "@<sub>:localhost"}
```
Response: `200 {}`

**POST /join/{roomId}** request: empty body `{}`
Response:
```json
{"room_id": "!<id>:localhost"}
```

**PUT /rooms/{roomId}/send/m.room.message/{txnId}** request:
```json
{"msgtype": "m.text", "body": "hello"}
```
Response:
```json
{"event_id": "$<hash>"}
```

**GET /rooms/{roomId}/messages** response:
```json
{
  "chunk": [
    {"event_id": "...", "type": "m.room.message", "sender": "...", "content": {"msgtype": "m.text", "body": "hello"}, ...}
  ],
  "start": "...",
  "end": "..."
}
```
The `"hello"` string appears in the `body` field of a `content` object. Asserting `strings.Contains(lastBody, "hello")` is sufficient for AC 1.

For idempotency (AC 2): assert `strings.Count(lastBody, "hello") == 1` to verify the message appears exactly once.

### Feature File: gateway/features/room_flow.feature

```gherkin
Feature: Room Flow — Create, Send, Receive
  As a developer
  I want to verify the full chat room lifecycle
  So that CI catches any regression in the core messaging path

  Scenario: User creates a room, sends a message, and another user receives it
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    When kai creates a room named "test-room"
    And kai invites alex to the room
    And alex joins the room
    And kai sends the message "hello" to the room
    Then the response status is 200
    And the response body contains "event_id"
    When alex retrieves messages from the room
    Then the response status is 200
    And the response body contains "hello"

  Scenario: Sending the same txnId twice returns the same event_id
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    When kai creates a room named "idempotency-test"
    And kai sends the message "idempotent" to the room
    And kai sends the same message again with the same txnId
    Then both sends returned the same event_id
    When kai retrieves messages from the room
    Then the response body contains "idempotent" exactly once
```

### room_flow_steps_test.go Implementation Guide

```go
//go:build integration

package integration_test

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/cucumber/godog"
)

var lastRoomID string
var lastEventID string
var kaiAccessToken string
var alexAccessToken string
var kaiUserID string
var alexUserID string
var lastTxnID string
var lastSecondEventID string

// authenticateUser runs Dex auth flow and Matrix login, storing token+userID into out pointers.
func authenticateUser(username, password string, accessToken, userID *string) error {
    // Step 1: Dex authorization code flow (reuse iObtainDexTokenFor from auth_steps_test.go)
    if err := iObtainDexTokenFor(username, password); err != nil {
        return fmt.Errorf("Dex auth for %s: %w", username, err)
    }
    // Step 2: Matrix login (reuse iPostLoginWithDexToken from auth_steps_test.go)
    if err := iPostLoginWithDexToken(); err != nil {
        return err
    }
    if lastStatusCode != http.StatusOK {
        return fmt.Errorf("POST /login for %s: expected 200, got %d (body: %s)", username, lastStatusCode, lastBody)
    }
    // Step 3: Parse access_token and user_id
    var loginResp struct {
        AccessToken string `json:"access_token"`
        UserID      string `json:"user_id"`
    }
    if err := json.Unmarshal([]byte(lastBody), &loginResp); err != nil {
        return fmt.Errorf("parsing /login response for %s: %w", username, err)
    }
    if loginResp.AccessToken == "" {
        return fmt.Errorf("empty access_token for %s", username)
    }
    *accessToken = loginResp.AccessToken
    *userID = loginResp.UserID
    return nil
}

func kaiIsAuthenticated() error {
    return authenticateUser("kai@example.com", "changeme", &kaiAccessToken, &kaiUserID)
}

func alexIsAuthenticated() error {
    return authenticateUser("alex@example.com", "changeme", &alexAccessToken, &alexUserID)
}

func kaiCreatesARoom(name string) error {
    payload := fmt.Sprintf(`{"name":%q}`, name)
    req, err := http.NewRequest(http.MethodPost, matrixURL+"/_matrix/client/v3/createRoom", strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("building createRoom request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /createRoom failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("createRoom returned %d: %s", resp.StatusCode, string(body))
    }
    var cr struct {
        RoomID string `json:"room_id"`
    }
    if err := json.Unmarshal(body, &cr); err != nil || cr.RoomID == "" {
        return fmt.Errorf("no room_id in createRoom response: %s", string(body))
    }
    lastRoomID = cr.RoomID
    return nil
}

func kaiInvitesAlex() error {
    payload := fmt.Sprintf(`{"user_id":%q}`, alexUserID)
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/invite", matrixURL, lastRoomID)
    req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("building invite request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /invite failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("invite returned %d: %s", resp.StatusCode, string(body))
    }
    return nil
}

func alexJoinsTheRoom() error {
    url := fmt.Sprintf("%s/_matrix/client/v3/join/%s", matrixURL, lastRoomID)
    req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
    if err != nil {
        return fmt.Errorf("building join request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+alexAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /join failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("join returned %d: %s", resp.StatusCode, string(body))
    }
    return nil
}

func kaiSendsMessage(msgBody string) error {
    lastTxnID = fmt.Sprintf("txn-%d", time.Now().UnixNano())
    payload := fmt.Sprintf(`{"msgtype":"m.text","body":%q}`, msgBody)
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s", matrixURL, lastRoomID, lastTxnID)
    req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("building sendEvent request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("PUT /send failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode == http.StatusOK {
        var sr struct {
            EventID string `json:"event_id"`
        }
        if err := json.Unmarshal(body, &sr); err == nil {
            lastEventID = sr.EventID
        }
    }
    return nil
}

func kaiSendsTheSameMessageAgain() error {
    // Reuse lastTxnID — this is the idempotency test
    payload := `{"msgtype":"m.text","body":"idempotent"}`
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s", matrixURL, lastRoomID, lastTxnID)
    req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
    if err != nil {
        return fmt.Errorf("building second sendEvent request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("PUT /send (second) failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    if resp.StatusCode == http.StatusOK {
        var sr struct {
            EventID string `json:"event_id"`
        }
        if err := json.Unmarshal(body, &sr); err == nil {
            lastSecondEventID = sr.EventID
        }
    }
    return nil
}

func alexGetsMessages() error {
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/messages", matrixURL, lastRoomID)
    req, err := http.NewRequest(http.MethodGet, url, nil)
    if err != nil {
        return fmt.Errorf("building getMessages request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+alexAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("GET /messages failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    return nil
}

func kaiGetsMessages() error {
    url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/messages", matrixURL, lastRoomID)
    req, err := http.NewRequest(http.MethodGet, url, nil)
    if err != nil {
        return fmt.Errorf("building getMessages request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("GET /messages failed: %w", err)
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastBody = string(body)
    return nil
}

func bothSendsReturnedTheSameEventID() error {
    if lastEventID == "" {
        return fmt.Errorf("lastEventID is empty — first send did not return event_id")
    }
    if lastSecondEventID == "" {
        return fmt.Errorf("lastSecondEventID is empty — second send did not return event_id")
    }
    if lastEventID != lastSecondEventID {
        return fmt.Errorf("txnId idempotency failed: first event_id=%q, second event_id=%q", lastEventID, lastSecondEventID)
    }
    return nil
}

func theBodyContainsExactlyOnce(substr string) error {
    count := strings.Count(lastBody, substr)
    if count != 1 {
        return fmt.Errorf("expected %q exactly once in body, found %d times; body: %s", substr, count, lastBody)
    }
    return nil
}

func initializeRoomFlowSteps(sc *godog.ScenarioContext) {
    sc.Step(`^kai is authenticated via OIDC$`, kaiIsAuthenticated)
    sc.Step(`^alex is authenticated via OIDC$`, alexIsAuthenticated)
    sc.Step(`^kai creates a room named "([^"]*)"$`, kaiCreatesARoom)
    sc.Step(`^kai invites alex to the room$`, kaiInvitesAlex)
    sc.Step(`^alex joins the room$`, alexJoinsTheRoom)
    sc.Step(`^kai sends the message "([^"]*)" to the room$`, kaiSendsMessage)
    sc.Step(`^kai sends the same message again with the same txnId$`, kaiSendsTheSameMessageAgain)
    sc.Step(`^alex retrieves messages from the room$`, alexGetsMessages)
    sc.Step(`^kai retrieves messages from the room$`, kaiGetsMessages)
    sc.Step(`^both sends returned the same event_id$`, bothSendsReturnedTheSameEventID)
    sc.Step(`^the response body contains "([^"]*)" exactly once$`, theBodyContainsExactlyOnce)
}
```

### steps_test.go Change — Add initializeRoomFlowSteps

Add exactly one line to `InitializeScenario`:

```go
func InitializeScenario(sc *godog.ScenarioContext) {
    sc.Step(`^the docker compose stack is started$`, theDockerComposeStackIsStarted)
    sc.Step(`^I call GET (/\S+) on the gateway$`, iCallGETOnGateway)
    sc.Step(`^I call GET :4000(/\S+) on the core$`, iCallGETOnCore)
    sc.Step(`^the response status is (\d+)$`, theResponseStatusIs)
    sc.Step(`^the response body contains "([^"]*)"$`, theResponseBodyContains)
    initializeAuthSteps(sc)           // auth scenario step definitions
    initializeAdminBootstrapSteps(sc) // admin bootstrap + dashboard step definitions
    initializeRoomFlowSteps(sc)       // room flow step definitions  ← ADD THIS
}
```

### Architecture Compliance

- **No new Go production code** — test-only files, no production binary changes
- **Feature file at `gateway/features/`** — established canonical location; NOT `test/features/` despite epics.md saying `tests/features/`
- **Build tag `//go:build integration`** — required; matches all existing integration step files
- **Package `integration_test`** — matches existing package declaration
- **Real Dex tokens, not mocked** — use authorization code flow (ROPC not supported by Dex v2.41)
- **matrixURL (port 8008)** — all Matrix API calls; NOT gatewayURL (port 8080)
- **`Strict: true`** — already set; all steps in room_flow.feature MUST be registered
- **Step regex anchoring** — wrap all regex patterns with `^...$` anchors

### Known Pitfalls to Avoid

1. **Wrong port**: All Matrix endpoints are on `matrixURL` (port 8008), NOT `gatewayURL` (port 8080).
2. **Reusing `lastAccessToken`** from auth steps: `auth_steps_test.go` stores the login token in `lastAccessToken`. After `kaiIsAuthenticated` calls `iPostLoginWithDexToken()`, `lastAccessToken` will be set — copy it to `kaiAccessToken` before calling `alexIsAuthenticated` (which will overwrite `lastAccessToken` with alex's token).
3. **txnId uniqueness**: Generate `txnId` as `fmt.Sprintf("txn-%d", time.Now().UnixNano())`. This is unique per scenario run. For the idempotency test, save this value and use it BOTH times.
4. **Scenario ordering**: Godog runs scenarios in file order. Scenario 1 leaves state in `lastRoomID`. Scenario 2 MUST create its own room to be isolated (recommended), OR explicitly re-initialize all state at the start.
5. **user_id format in invite**: The `user_id` field in the invite body must be the full Matrix `user_id` from the login response (e.g., `@CiQw...:localhost`), NOT the email or UUID.
6. **Empty body for join**: `POST /join/{roomId}` requires a request body. Send `{}` (or `strings.NewReader("{}")`).
7. **GET /messages auth**: `GetMessagesHandler` requires a valid `Authorization: Bearer <token>` header. Anonymous access returns 401.

### Dex Users Available for Testing

| Email | Password | UUID | Groups | Encoded sub |
|-------|----------|------|--------|-------------|
| `kai@example.com` | `changeme` | `00000000-0000-0000-0000-000000000001` | `instance_admin` | `CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE` |
| `compliance@example.com` | `changeme` | `00000000-0000-0000-0000-000000000002` | `compliance_officer` | (protobuf-encoded UUID 2) |
| `alex@example.com` | `changeme` | `00000000-0000-0000-0000-000000000003` | `user` | (protobuf-encoded UUID 3) |

Use `kai` as creator/sender (has `instance_admin` rights, no permission restrictions) and `alex` as the second user (regular `user` role, can join and read).

### Project Structure — Files to Create/Modify

| File | Action |
|------|--------|
| `gateway/features/room_flow.feature` | CREATE |
| `gateway/test/integration/room_flow_steps_test.go` | CREATE |
| `gateway/test/integration/steps_test.go` | MODIFY — add `initializeRoomFlowSteps(sc)` call only |

**No changes to:** `Makefile`, `docker-compose.yml`, `dev/dex/config.yaml`, any production Go files, any Elixir files, any proto files, `main_test.go`, `auth_steps_test.go`, `admin_bootstrap_steps_test.go`.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-4.21] Full AC (authoritative)
- [Source: gateway/test/integration/auth_steps_test.go] Dex authorization code flow implementation (`iObtainDexTokenFor`), Matrix login pattern (`iPostLoginWithDexToken`), package-level state sharing
- [Source: gateway/test/integration/main_test.go] URL vars: `matrixURL`, `dexURL`, `gatewayURL`; `Strict: true` constraint
- [Source: gateway/test/integration/steps_test.go] `InitializeScenario` pattern; shared steps (`theResponseStatusIs`, `theResponseBodyContains`)
- [Source: gateway/features/auth.feature] Established feature file format and step naming conventions
- [Source: gateway/internal/matrix/rooms.go] `CreateRoomHandler`, `InviteUserHandler`, `JoinRoomHandler`, `SendEventHandler` — request/response shapes
- [Source: gateway/internal/matrix/messages.go] `GetMessagesHandler` — response format with `chunk` array
- [Source: dev/dex/config.yaml] Dex users, client credentials, `redirect_uri` requirement
- [Source: 2-21-gherkin-auth-scenario-end-to-end-oidc-login.md#Dev-Notes] Dex ROPC not supported; sub is protobuf-encoded; Matrix API on port 8008; `matrixURL` in `TestMain`
- [Source: Makefile#test-integration] `--network=nebu_default`; env vars passed to test container; all URLs injected

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m] (Claude Code)

### Debug Log References

- Verified both feature file and step definitions were already fully implemented (not pending stubs as the story prompt suggested).
- Confirmed `steps_test.go` already contained the `initializeRoomFlowSteps(sc)` call.
- Ran `go build -tags integration ./test/integration/...` — compiled successfully with no errors.
- Cross-checked all 14 step patterns in `room_flow.feature` against registered `sc.Step(...)` entries; every step has a matching registration.

### Completion Notes List

- `gateway/features/room_flow.feature`: Feature file with two scenarios — full room create/invite/join/send/messages flow and txnId idempotency scenario.
- `gateway/test/integration/room_flow_steps_test.go`: Complete step definitions implementing all 11 room-flow steps. Uses `authenticateUser` helper that reuses `iObtainDexTokenFor` and `iPostLoginWithDexToken` from `auth_steps_test.go`. All Matrix API calls go to `matrixURL` (port 8008). Step implementations: `kaiIsAuthenticated`, `alexIsAuthenticated`, `kaiCreatesARoom`, `kaiInvitesAlex`, `alexJoinsTheRoom`, `kaiSendsMessage`, `kaiSendsTheSameMessageAgain`, `alexRetrievesMessagesFromTheRoom`, `kaiRetrievesMessagesFromTheRoom`, `bothSendsReturnedTheSameEventID`, `theBodyContainsExactlyOnce`.
- `gateway/test/integration/steps_test.go`: `initializeRoomFlowSteps(sc)` call already present in `InitializeScenario`; no changes required.
- Task 4 (live `make test-integration` run) requires the full Docker stack — cannot be executed in this environment. Code compiles cleanly and all step patterns are verified to match the feature file exactly. AC 5 is contingent on the live stack run.

### File List

- `gateway/features/room_flow.feature` (CREATED)
- `gateway/test/integration/room_flow_steps_test.go` (CREATED)
- `gateway/test/integration/steps_test.go` (already contained `initializeRoomFlowSteps(sc)` — no modification needed)

## Change Log

- 2026-04-03: Story created by Bob (SM) — comprehensive Gherkin end-to-end room flow test for Epic 4 completion gate.
- 2026-04-03: Implemented by Amelia (Dev) — verified feature file and step definitions complete, code compiles cleanly, all step patterns matched. Status set to review.
