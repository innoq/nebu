//go:build integration

package integration_test

// ─── Story 9-28: Thread Relations — GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread ───
//
// Bug: the first thread reply (opening a new thread) does not appear in Element Web's
// thread panel. The recipient sees a thread notification but no content.
// Root cause: missing /relations endpoint + missing unsigned.m.relations.m.thread in /sync.
//
// State shared from room_flow_steps_test.go / steps_test.go:
//   - kaiAccessToken, alexAccessToken, marieAccessToken
//   - lastRoomID     — set by room creation steps
//   - lastEventID    — set by message-send steps (used as thread root)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

var lastThreadReplyEventID string

// alexSendsThreadReplyToKaisMessage sends a thread reply event referencing lastEventID
// (the most recent event kai sent) as the thread root.
func alexSendsThreadReplyToKaisMessage() error {
	threadRootID := lastEventID
	if threadRootID == "" {
		return fmt.Errorf("no lastEventID set — kai must send a message first")
	}

	txnID := fmt.Sprintf("thread-reply-txn-%d", time.Now().UnixNano())
	payload := fmt.Sprintf(`{
		"msgtype": "m.text",
		"body": "> <@kai:nebu.local> hello\n\nFirst thread reply",
		"m.relates_to": {
			"rel_type": "m.thread",
			"event_id": %q,
			"is_falling_back": true,
			"m.in_reply_to": {"event_id": %q}
		}
	}`, threadRootID, threadRootID)

	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, lastRoomID, txnID)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building thread reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT thread reply: %w", err)
	}
	defer resp.Body.Close()

	if err := captureResponse(resp); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected 200 sending thread reply, got %d: %s", resp.StatusCode, lastBody)
	}

	var sr struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(lastBody), &sr); err == nil {
		lastThreadReplyEventID = sr.EventID
	}
	return nil
}

// alexCallsGetThreadRelations calls GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread.
// Uses lastEventID as the thread root event ID.
func alexCallsGetThreadRelations() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations: %w", err)
	}
	return captureResponse(resp)
}

// marieCallsGetThreadRelations calls the /relations endpoint as marie (non-member).
func marieCallsGetThreadRelations() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations (marie): %w", err)
	}
	return captureResponse(resp)
}

// unauthenticatedClientCallsGetThreadRelations calls /relations without a token.
func unauthenticatedClientCallsGetThreadRelations() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread",
		matrixURL, lastRoomID, lastEventID)
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("GET /relations (unauth): %w", err)
	}
	return captureResponse(resp)
}

// alexCallsGetThreadRelationsUnknownRoot calls /relations with a non-existent event ID.
func alexCallsGetThreadRelationsUnknownRoot() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/$unknown_thread_root_9_28:nebu.local/m.thread",
		matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations unknown: %w", err)
	}
	return captureResponse(resp)
}

// alexCallsGetSync performs GET /_matrix/client/v3/sync for alex.
func alexCallsGetSync() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/sync", matrixURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync: %w", err)
	}
	return captureResponse(resp)
}

// theRelationsResponseContainsThreadReply checks that lastBody contains the thread reply event ID.
func theRelationsResponseContainsThreadReply() error {
	if lastThreadReplyEventID == "" {
		return fmt.Errorf("no lastThreadReplyEventID set — thread reply must be sent first")
	}
	if !strings.Contains(lastBody, lastThreadReplyEventID) {
		return fmt.Errorf("expected /relations response to contain thread reply event_id %q, got: %s",
			lastThreadReplyEventID, lastBody)
	}
	// Also verify the chunk array exists.
	if !strings.Contains(lastBody, `"chunk"`) {
		return fmt.Errorf("expected /relations response to have 'chunk' key, got: %s", lastBody)
	}
	return nil
}

// theRelationsResponseChunkIsEmpty checks that lastBody has an empty chunk array.
func theRelationsResponseChunkIsEmpty() error {
	var resp struct {
		Chunk []json.RawMessage `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parsing /relations response: %w (body: %s)", err, lastBody)
	}
	if len(resp.Chunk) != 0 {
		return fmt.Errorf("expected empty chunk, got %d events: %s", len(resp.Chunk), lastBody)
	}
	return nil
}

// theSyncIncludesMThreadBundledAggregation checks that the parent event in the sync
// response carries unsigned.m.relations.m.thread with count >= 1 and latest_event set.
func theSyncIncludesMThreadBundledAggregation() error {
	if lastEventID == "" {
		return fmt.Errorf("no lastEventID set — kai must send a message first")
	}

	// Parse the /sync response body.
	var syncBody struct {
		Rooms struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						EventID  string `json:"event_id"`
						Unsigned struct {
							MRelations struct {
								MThread *struct {
									Count       int             `json:"count"`
									LatestEvent json.RawMessage `json:"latest_event"`
								} `json:"m.thread"`
							} `json:"m.relations"`
						} `json:"unsigned"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}

	if err := json.Unmarshal([]byte(lastBody), &syncBody); err != nil {
		return fmt.Errorf("parsing /sync response: %w (body: %.200s)", err, lastBody)
	}

	room, ok := syncBody.Rooms.Join[lastRoomID]
	if !ok {
		return fmt.Errorf("room %s not found in sync rooms.join: %s", lastRoomID, lastBody)
	}

	for _, ev := range room.Timeline.Events {
		if ev.EventID == lastEventID {
			mThread := ev.Unsigned.MRelations.MThread
			if mThread == nil {
				return fmt.Errorf("parent event %s has no unsigned.m.relations.m.thread: %s", lastEventID, lastBody)
			}
			if mThread.Count < 1 {
				return fmt.Errorf("expected m.thread.count >= 1, got %d", mThread.Count)
			}
			if len(mThread.LatestEvent) == 0 || string(mThread.LatestEvent) == "null" {
				return fmt.Errorf("expected m.thread.latest_event to be set, got: %s", mThread.LatestEvent)
			}
			return nil
		}
	}

	return fmt.Errorf("parent event %s not found in sync timeline for room %s", lastEventID, lastRoomID)
}

// alexCallsGetThreadRelationsWithRecurse calls GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread?dir=b&recurse=true.
// Regression test for Story 11-8: missing rpc :GetRelations in core_grpc.pb.ex caused 500.
func alexCallsGetThreadRelationsWithRecurse() error {
	url := fmt.Sprintf("%s/_matrix/client/v1/rooms/%s/relations/%s/m.thread?dir=b&recurse=true",
		matrixURL, lastRoomID, lastEventID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /relations?dir=b&recurse=true: %w", err)
	}
	return captureResponse(resp)
}

func initializeThreadRelationsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai has sent a message in the room$`, func() error {
		return kaiSendsMessage("Hello from kai")
	})
	sc.Step(`^alex sends a thread reply to kai's message$`, alexSendsThreadReplyToKaisMessage)
	sc.Step(`^alex calls GET /rooms/\{roomId\}/relations/\{eventId\}/m\.thread$`, alexCallsGetThreadRelations)
	sc.Step(`^marie calls GET /rooms/\{roomId\}/relations/\{eventId\}/m\.thread$`, marieCallsGetThreadRelations)
	sc.Step(`^an unauthenticated client calls GET /rooms/\{roomId\}/relations/\{eventId\}/m\.thread$`, unauthenticatedClientCallsGetThreadRelations)
	sc.Step(`^alex calls GET /rooms/\{roomId\}/relations/\$unknown_thread_root_9_28/m\.thread$`, alexCallsGetThreadRelationsUnknownRoot)
	sc.Step(`^alex calls GET /sync$`, alexCallsGetSync)
	sc.Step(`^the relations response contains the thread reply event$`, theRelationsResponseContainsThreadReply)
	sc.Step(`^the relations response chunk is empty$`, theRelationsResponseChunkIsEmpty)
	sc.Step(`^the sync response includes m\.thread bundled aggregation on the parent event$`, theSyncIncludesMThreadBundledAggregation)
	sc.Step(`^alex calls GET /rooms/\{roomId\}/relations/\{eventId\}/m\.thread\?dir=b&recurse=true$`, alexCallsGetThreadRelationsWithRecurse)
}
