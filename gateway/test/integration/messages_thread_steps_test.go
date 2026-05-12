//go:build integration

package integration_test

// ─── Story 9-29: Thread Message Relations — GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}[/{relType}[/{eventType}]] ───
//
// Bug: Element Web sends GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}
// (no relType) and receives 404 because only the /{relType} variant is currently registered.
//
// Matrix CS API v1 requires all three variants to be registered:
//   1. /relations/{eventId}                        ← MISSING (causes Element Web 404)
//   2. /relations/{eventId}/{relType}               ← exists but incomplete
//   3. /relations/{eventId}/{relType}/{eventType}   ← MISSING
//
// State shared from room_flow_steps_test.go / steps_test.go:
//   - kaiAccessToken, alexAccessToken, marieAccessToken
//   - lastRoomID     — set by room creation steps
//   - lastEventID    — set by message-send steps (used as thread root)
//
// All step functions in this file are registered via initializeMessagesThreadSteps.
// These tests are RED until the implementation adds all three route variants.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// firstThreadReplyEventID holds the event_id of the first (oldest) thread reply sent,
// used by the dir=f ordering assertion to verify oldest-first ordering.
var firstThreadReplyEventID string

// ─── Route variant 1: /relations/{eventId}  (no relType) ───

// alexCallsGetRelationsBaseRoute calls GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}
// with no relType. This is the route that was missing and caused the Element Web 404.
func alexCallsGetRelationsBaseRoute() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations (base, no relType): %w", err)
	}
	return captureResponse(resp)
}

// marieCallsGetRelationsBaseRoute calls the base /relations/{eventId} route as marie (non-member).
func marieCallsGetRelationsBaseRoute() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations (base, marie): %w", err)
	}
	return captureResponse(resp)
}

// unauthenticatedClientCallsGetRelationsBaseRoute calls /relations/{eventId} without any token.
func unauthenticatedClientCallsGetRelationsBaseRoute() error {
	if lastEventID == "" {
		return fmt.Errorf("lastEventID is empty — kai must send a message first")
	}
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s",
		matrixURL, lastRoomID, lastEventID)
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("GET /relations (base, unauth): %w", err)
	}
	return captureResponse(resp)
}

// alexSendsSecondThreadReplyToKaisMessage sends a second m.thread reply to the thread root
// (lastEventID). It saves the current lastThreadReplyEventID as firstThreadReplyEventID
// before overwriting, enabling the dir=f oldest-first ordering assertion.
func alexSendsSecondThreadReplyToKaisMessage() error {
	firstThreadReplyEventID = lastThreadReplyEventID
	return alexSendsThreadReplyToKaisMessage()
}

// alexCallsGetRelationsBaseRouteUnknownEvent calls /relations with a non-existent event ID.
func alexCallsGetRelationsBaseRouteUnknownEvent() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/$unknown_event_9_29:nebu.local",
		matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations (base, unknown): %w", err)
	}
	return captureResponse(resp)
}

// ─── Route variant 2: /relations/{eventId}/{relType}?{query} ───

// alexCallsGetRelationsMThreadWithQuery calls GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread
// appending the given raw query string (e.g. "dir=b", "dir=f", "recurse=true", "dir=invalid").
func alexCallsGetRelationsMThreadWithQuery(rawQuery string) error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread?%s",
		matrixURL, lastRoomID, lastEventID, rawQuery)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations/m.thread?%s: %w", rawQuery, err)
	}
	return captureResponse(resp)
}

// ─── Route variant 3: /relations/{eventId}/{relType}/{eventType} ───

// alexCallsGetRelationsThreeSegment calls GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread/m.room.message
func alexCallsGetRelationsThreeSegment() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread/m.room.message",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations/m.thread/m.room.message: %w", err)
	}
	return captureResponse(resp)
}

// ─── Assertions ───

// theRelationsResponseChunkContainsThreadReplyEvent checks that lastBody contains
// the thread reply event_id stored in lastThreadReplyEventID.
func theRelationsResponseChunkContainsThreadReplyEvent() error {
	if lastThreadReplyEventID == "" {
		return fmt.Errorf("no lastThreadReplyEventID — thread reply must be sent first")
	}
	if !strings.Contains(lastBody, lastThreadReplyEventID) {
		return fmt.Errorf(
			"expected /relations chunk to contain thread reply event_id %q, got: %s",
			lastThreadReplyEventID, lastBody,
		)
	}
	return nil
}

// theRelationsResponseHasChunkArray verifies that lastBody is valid JSON with a "chunk" key.
func theRelationsResponseHasChunkArray(key string) error {
	if !strings.Contains(lastBody, `"chunk"`) {
		return fmt.Errorf("expected /relations response to have %q key, got: %s", key, lastBody)
	}
	var resp struct {
		Chunk []json.RawMessage `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parsing /relations response for %q: %w (body: %s)", key, err, lastBody)
	}
	return nil
}

// theRelationsChunkIsNotEmpty verifies the chunk array has at least one event.
func theRelationsChunkIsNotEmpty() error {
	var resp struct {
		Chunk []json.RawMessage `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parsing /relations response for non-empty check: %w (body: %s)", err, lastBody)
	}
	if len(resp.Chunk) == 0 {
		return fmt.Errorf("expected non-empty chunk in /relations response, got empty: %s", lastBody)
	}
	return nil
}

// theFirstChunkEventIsTheMostRecentReply asserts chunk[0].event_id == lastThreadReplyEventID
// (dir=b = newest-first: the most recently sent reply must be first).
func theFirstChunkEventIsTheMostRecentReply() error {
	if lastThreadReplyEventID == "" {
		return fmt.Errorf("lastThreadReplyEventID is empty — thread reply must be sent first")
	}
	var resp struct {
		Chunk []struct {
			EventID string `json:"event_id"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse /relations body for dir=b ordering: %w (body: %s)", err, lastBody)
	}
	if len(resp.Chunk) == 0 {
		return fmt.Errorf("chunk is empty, cannot verify dir=b ordering")
	}
	if resp.Chunk[0].EventID != lastThreadReplyEventID {
		return fmt.Errorf("dir=b: expected chunk[0].event_id=%q (newest), got %q (body: %s)",
			lastThreadReplyEventID, resp.Chunk[0].EventID, lastBody)
	}
	return nil
}

// theFirstChunkEventIsTheOldestReply asserts chunk[0].event_id == firstThreadReplyEventID
// (dir=f = oldest-first: the first-sent reply must be first).
func theFirstChunkEventIsTheOldestReply() error {
	if firstThreadReplyEventID == "" {
		return fmt.Errorf("firstThreadReplyEventID is empty — two thread replies must be sent first")
	}
	var resp struct {
		Chunk []struct {
			EventID string `json:"event_id"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse /relations body for dir=f ordering: %w (body: %s)", err, lastBody)
	}
	if len(resp.Chunk) == 0 {
		return fmt.Errorf("chunk is empty, cannot verify dir=f ordering")
	}
	if resp.Chunk[0].EventID != firstThreadReplyEventID {
		return fmt.Errorf("dir=f: expected chunk[0].event_id=%q (oldest), got %q (body: %s)",
			firstThreadReplyEventID, resp.Chunk[0].EventID, lastBody)
	}
	return nil
}

// ─── Step registration ───

func initializeMessagesThreadSteps(sc *godog.ScenarioContext) {
	// Note: all Background steps are re-used from existing registrations:
	//   - "kai is authenticated via OIDC"                     → room_flow_steps_test.go
	//   - "alex is authenticated via OIDC"                    → room_flow_steps_test.go
	//   - "marie is authenticated via OIDC"                   → room_flow_steps_test.go
	//   - "kai creates a room named ..."                      → room_flow_steps_test.go
	//   - "kai invites alex to the room"                      → room_flow_steps_test.go
	//   - "alex joins the room"                               → room_flow_steps_test.go
	//   - "kai has sent a message in the room"                → thread_relations_steps_test.go
	//   - "alex sends a thread reply to kai's message"        → thread_relations_steps_test.go
	//   - "the response status is ..."                        → steps_test.go
	//   - "the response body has errcode ..."                 → steps_test.go (via upgrade_room)
	// No duplicate registrations are made here.

	// AC1: base route — GET /relations/{eventId} (no relType)
	sc.Step(`^alex calls GET /relations/\{eventId\} without relType$`, alexCallsGetRelationsBaseRoute)
	sc.Step(`^the relations response chunk contains the thread reply event$`, theRelationsResponseChunkContainsThreadReplyEvent)
	sc.Step(`^the relations response has a "([^"]*)" array$`, theRelationsResponseHasChunkArray)

	// AC2 / AC3 / AC4 / AC9: GET /relations/{eventId}/m.thread?{query}
	sc.Step(`^alex sends a second thread reply to kai's message$`, alexSendsSecondThreadReplyToKaisMessage)
	sc.Step(`^alex calls GET /relations/\{eventId\}/m\.thread with query "([^"]*)"$`, alexCallsGetRelationsMThreadWithQuery)
	sc.Step(`^the relations response chunk is not empty$`, theRelationsChunkIsNotEmpty)
	sc.Step(`^the first chunk event is the most recent reply$`, theFirstChunkEventIsTheMostRecentReply)
	sc.Step(`^the first chunk event is the oldest reply$`, theFirstChunkEventIsTheOldestReply)

	// AC5: three-segment route — GET /relations/{eventId}/{relType}/{eventType}
	sc.Step(`^alex calls GET /relations/\{eventId\}/m\.thread/m\.room\.message$`, alexCallsGetRelationsThreeSegment)

	// AC6: 403 for non-member
	sc.Step(`^marie calls GET /relations/\{eventId\} without relType$`, marieCallsGetRelationsBaseRoute)

	// AC7: 401 for unauthenticated
	sc.Step(`^an unauthenticated client calls GET /relations/\{eventId\} without relType$`, unauthenticatedClientCallsGetRelationsBaseRoute)

	// AC8: 404 for unknown eventId
	sc.Step(`^alex calls GET /relations/\$unknown_event_9_29 without relType$`, alexCallsGetRelationsBaseRouteUnknownEvent)
}
