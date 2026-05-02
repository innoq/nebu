//go:build integration

package integration_test

// ─── Story 7-28: Event Context — GET /rooms/{roomId}/context/{eventId} ────────
//
// Godog step definitions for gateway/features/event_context.feature.
//
// State shared from room_flow_steps_test.go / steps_test.go:
//   - kaiAccessToken, marieAccessToken — set by OIDC auth steps
//   - lastRoomID — set by room creation steps
//   - lastEventID — set by message-send steps
//   - lastStatusCode, lastBody — from steps_test.go

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

func kaiCallsGetEventContext(limit string) error {
	url := matrixURL + "/_matrix/client/v3/rooms/" + lastRoomID + "/context/" + lastEventID
	if limit != "" {
		url += "?limit=" + limit
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return captureResponse(resp)
}

func marieCallsGetEventContext() error {
	url := matrixURL + "/_matrix/client/v3/rooms/" + lastRoomID + "/context/" + lastEventID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s (marie): %w", url, err)
	}
	return captureResponse(resp)
}

func unauthenticatedClientCallsGetEventContext() error {
	url := matrixURL + "/_matrix/client/v3/rooms/" + lastRoomID + "/context/" + lastEventID
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("GET %s (unauth): %w", url, err)
	}
	return captureResponse(resp)
}

func kaiCallsGetUnknownEventContext() error {
	url := matrixURL + "/_matrix/client/v3/rooms/" + lastRoomID + "/context/$nonexistent_event_id_7_28:nebu.local"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return captureResponse(resp)
}

func theContextResponseContainsField(field string) error {
	if !strings.Contains(lastBody, field) {
		return fmt.Errorf("expected field %q in context response, got: %s", field, lastBody)
	}
	return nil
}

func initializeEventContextSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /rooms/\{roomId\}/context/\{eventId\}\?limit=(\d+)$`, func(limit string) error {
		return kaiCallsGetEventContext(limit)
	})
	sc.Step(`^kai calls GET /rooms/\{roomId\}/context/\{eventId\}$`, func() error {
		return kaiCallsGetEventContext("")
	})
	sc.Step(`^marie calls GET /rooms/\{roomId\}/context/\{eventId\}$`, marieCallsGetEventContext)
	sc.Step(`^an unauthenticated client calls GET /rooms/\{roomId\}/context/\{eventId\}$`, unauthenticatedClientCallsGetEventContext)
	sc.Step(`^kai calls GET /rooms/\{roomId\}/context/\$nonexistent_event_id_7_28$`, kaiCallsGetUnknownEventContext)
	sc.Step(`^the context response contains (.+)$`, theContextResponseContainsField)
	sc.Step(`^the response body is a JSON object$`, func() error {
		if len(lastBody) == 0 || lastBody[0] != '{' {
			return fmt.Errorf("expected JSON object, got: %s", lastBody)
		}
		return nil
	})
}
