//go:build integration

package integration_test

// Story 13-6: Core Clustering — Godog step definitions for core_clustering.feature
//
// RED PHASE: These step definitions are registered but return errors until
// the full implementation is in place:
//   1. docker-compose.scale.yml must define core2 with CLUSTER_NODES env var.
//   2. Core runtime.exs must configure libcluster (Gossip or Epmd strategy).
//   3. The Docker stop + poll-for-recovery logic below must run against a live
//      2-core stack (use @scale tag to skip when only a 1-core stack is up).
//
// Build tag: integration — run with:
//   go test -tags=integration ./gateway/test/integration/... -run TestIntegrationSuite
//
// The @scale Gherkin tag is honoured by CI only when NEBU_SCALE_TEST=true is set.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// clusteringRoomID is the room created during the failover scenario.
var clusteringRoomID string

// clusteringAccessToken is the Matrix access token used for the failover scenario.
var clusteringAccessToken string

// clusteringEventID is the event_id of the last message sent during the failover scenario.
var clusteringEventID string

// the2CoreDockerComposeStackIsRunning verifies that both core1 and core2 containers are up.
// RED PHASE: returns an error because the core2 service does not yet exist.
func the2CoreDockerComposeStackIsRunning() error {
	out, err := exec.Command("docker", "compose",
		"-f", "docker-compose.yml",
		"-f", "docker-compose.scale.yml",
		"ps", "--format", "json").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("docker compose ps failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	foundCore1 := false
	foundCore2 := false

	for _, line := range lines {
		if line == "" {
			continue
		}
		var svc struct {
			Service string `json:"Service"`
			State   string `json:"State"`
		}
		if err := json.Unmarshal([]byte(line), &svc); err != nil {
			continue
		}
		if svc.Service == "core" && svc.State == "running" {
			foundCore1 = true
		}
		if svc.Service == "core2" && svc.State == "running" {
			foundCore2 = true
		}
	}

	if !foundCore1 {
		return fmt.Errorf("core (core1) is not running — start with: docker compose -f docker-compose.yml -f docker-compose.scale.yml up -d")
	}
	if !foundCore2 {
		return fmt.Errorf("core2 is not running — docker-compose.scale.yml must define a core2 service (Story 13-6 AC5)")
	}
	return nil
}

// aRoomExistsAndAMessageHasBeenSent creates a room and sends one message via the Matrix API.
// Uses the shared auth token from previous scenarios or obtains a fresh one.
func aRoomExistsAndAMessageHasBeenSent() error {
	// Reuse an existing token from auth_steps or obtain one now.
	token := clusteringAccessToken
	if token == "" {
		// Obtain Dex token and login.
		if err := iObtainDexTokenFor("admin@example.com", "admin"); err != nil {
			return fmt.Errorf("dex auth for clustering scenario: %w", err)
		}
		if err := iPostLoginWithDexToken(); err != nil {
			return fmt.Errorf("matrix login for clustering scenario: %w", err)
		}
		token = lastAccessToken
		clusteringAccessToken = token
	}

	// Create a room.
	body := `{"name":"Clustering Test Room","preset":"private_chat"}`
	req, err := http.NewRequest("POST", matrixURL+"/_matrix/client/v3/createRoom", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building createRoom request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("createRoom request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("createRoom: expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	var createResp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return fmt.Errorf("parsing createRoom response: %w", err)
	}
	clusteringRoomID = createResp.RoomID

	// Send a message to the room.
	msgBody := `{"msgtype":"m.text","body":"pre-failover message"}`
	txnID := fmt.Sprintf("txn-cluster-prefailover-%d", time.Now().UnixNano())
	sendURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, clusteringRoomID, txnID)

	sendReq, err := http.NewRequest("PUT", sendURL, strings.NewReader(msgBody))
	if err != nil {
		return fmt.Errorf("building send request: %w", err)
	}
	sendReq.Header.Set("Authorization", "Bearer "+token)
	sendReq.Header.Set("Content-Type", "application/json")

	sendResp, err := http.DefaultClient.Do(sendReq)
	if err != nil {
		return fmt.Errorf("send_event request failed: %w", err)
	}
	defer sendResp.Body.Close()

	sendRespBody, _ := io.ReadAll(sendResp.Body)
	if sendResp.StatusCode != http.StatusOK {
		return fmt.Errorf("send_event: expected 200, got %d: %s", sendResp.StatusCode, sendRespBody)
	}

	var sendResult struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(sendRespBody, &sendResult); err != nil {
		return fmt.Errorf("parsing send_event response: %w", err)
	}
	clusteringEventID = sendResult.EventID

	return nil
}

// coreInstance1IsStopped stops the core (core1) container.
// RED PHASE: Will fail if core2 is not running (no failover target).
func coreInstance1IsStopped() error {
	// Identify the core1 container name from docker compose ps.
	out, err := exec.Command("docker", "compose",
		"-f", "docker-compose.yml",
		"-f", "docker-compose.scale.yml",
		"ps", "--format", "json").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("docker compose ps failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var core1ContainerName string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var svc struct {
			Service string `json:"Service"`
			Name    string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(line), &svc); err != nil {
			continue
		}
		// The core service in base docker-compose is named "core".
		if svc.Service == "core" {
			core1ContainerName = svc.Name
			break
		}
	}

	if core1ContainerName == "" {
		return fmt.Errorf("could not find core1 container name from docker compose ps output")
	}

	stopCmd := exec.Command("docker", "stop", core1ContainerName) //nolint:gosec
	if out, err := stopCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker stop %s failed: %w (output: %s)", core1ContainerName, err, out)
	}

	return nil
}

// aNewMessageCanBeSentToTheRoomWithin10Seconds polls the room for up to 10 seconds
// after core1 is stopped, attempting to send a message. Passes when HTTP 200 is received.
func aNewMessageCanBeSentToTheRoomWithin10Seconds() error {
	token := clusteringAccessToken
	if token == "" {
		return fmt.Errorf("no access token — ensure 'a room exists and a message has been sent' ran first")
	}
	if clusteringRoomID == "" {
		return fmt.Errorf("no room ID — ensure 'a room exists and a message has been sent' ran first")
	}

	deadline := time.Now().Add(10 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		txnID := fmt.Sprintf("txn-cluster-failover-%d", time.Now().UnixNano())
		sendURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
			matrixURL, clusteringRoomID, txnID)
		msgBody := `{"msgtype":"m.text","body":"post-failover message"}`

		req, err := http.NewRequest("PUT", sendURL, strings.NewReader(msgBody))
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("send_event HTTP request failed: %w", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			lastStatusCode = resp.StatusCode
			lastBody = string(respBody)
			return nil
		}

		lastErr = fmt.Errorf("send_event returned HTTP %d: %s", resp.StatusCode, respBody)
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("room was not available within 10 seconds after core1 stopped: %w", lastErr)
}

// theMessageIsAcceptedWithHTTP200AndAnEventID verifies the last send_event
// response contained HTTP 200 and a non-empty event_id.
func theMessageIsAcceptedWithHTTP200AndAnEventID() error {
	if lastStatusCode != http.StatusOK {
		return fmt.Errorf("expected HTTP 200, got %d (body: %s)", lastStatusCode, lastBody)
	}

	var result struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(lastBody), &result); err != nil {
		return fmt.Errorf("parsing response body as JSON: %w (body: %s)", err, lastBody)
	}
	if result.EventID == "" {
		return fmt.Errorf("response body did not contain an event_id (body: %s)", lastBody)
	}

	return nil
}

// core1AndCore2AreConnectedInAHordeCluster checks the Core health endpoint
// for cluster membership. This is a lightweight smoke check.
// RED PHASE: Will fail until Core exposes a cluster-status endpoint or the
// clustering test can verify via Horde distributed API.
func core1AndCore2AreConnectedInAHordeCluster() error {
	// Check via the Core health endpoint whether cluster is formed.
	// The Core health endpoint must be extended in Story 13-6 to include
	// cluster_nodes in its response body.
	resp, err := http.Get(coreURL + "/health") //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s/health failed: %w", coreURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core health check returned %d: %s", resp.StatusCode, body)
	}

	// RED PHASE: The health endpoint does not yet return cluster_nodes.
	// This assertion will fail until the Core health handler is updated.
	if !strings.Contains(string(body), "cluster_nodes") {
		return fmt.Errorf(
			"core health response does not contain 'cluster_nodes' — " +
				"Core health endpoint must be updated to report Horde cluster membership (Story 13-6 AC4 / cluster smoke check). " +
				"Body: %s",
			body,
		)
	}

	return nil
}

// initializeClusteringSteps registers all step definitions for core_clustering.feature.
func initializeClusteringSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the 2-core docker compose stack is running$`, the2CoreDockerComposeStackIsRunning)
	sc.Step(`^a room exists and a message has been sent$`, aRoomExistsAndAMessageHasBeenSent)
	sc.Step(`^core instance 1 is stopped$`, coreInstance1IsStopped)
	sc.Step(`^a new message can be sent to the room within 10 seconds$`, aNewMessageCanBeSentToTheRoomWithin10Seconds)
	sc.Step(`^the message is accepted with HTTP 200 and an event_id$`, theMessageIsAcceptedWithHTTP200AndAnEventID)
	sc.Step(`^core1 and core2 are connected in a Horde cluster$`, core1AndCore2AreConnectedInAHordeCluster)
}
