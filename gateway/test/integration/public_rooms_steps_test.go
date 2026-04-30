//go:build integration

package integration_test

// ─── Story 7-27: Public Room Directory — GET/POST /publicRooms ──────────────
//
// Godog step definitions for gateway/features/public_rooms.feature.
//
// State shared from room_flow_steps_test.go / steps_test.go:
//   - kaiAccessToken — set by kaiIsAuthenticated
//   - lastStatusCode, lastBody — from steps_test.go

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

func unauthenticatedClientCallsGetPublicRooms(path string) error {
	url := matrixURL + path
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return captureResponse(resp)
}

func kaiCallsPostPublicRoomsWithBody(body string) error {
	url := matrixURL + "/_matrix/client/v3/publicRooms"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	return captureResponse(resp)
}

func unauthenticatedClientCallsPostPublicRoomsWithBody(body string) error {
	url := matrixURL + "/_matrix/client/v3/publicRooms"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s (unauth): %w", url, err)
	}
	return captureResponse(resp)
}

func initializePublicRoomsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^an unauthenticated client calls GET (/_matrix/client/v3/publicRooms[^\s]*)$`, unauthenticatedClientCallsGetPublicRooms)
	sc.Step(`^kai calls POST /_matrix/client/v3/publicRooms with body (.+)$`, kaiCallsPostPublicRoomsWithBody)
	sc.Step(`^an unauthenticated client calls POST /_matrix/client/v3/publicRooms with body (.+)$`, unauthenticatedClientCallsPostPublicRoomsWithBody)
}
