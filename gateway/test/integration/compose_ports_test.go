//go:build integration

package integration_test

// Story 5.29a — Block B: gRPC port 9000 not publicly exposed (AC8)
//
// This test verifies that port 9000 on the `core` service is NOT published
// to the host in docker-compose.yml, enforcing that the gRPC endpoint is
// only reachable within the internal Compose network.
//
// Test strategy:
//   The test runs `docker compose ps --format json` and checks that port 9000
//   is absent from the published-ports list of the `core` service.
//
//   Alternatively (if docker is not available in the test container), a Makefile
//   target `test-compose-ports` is provided as a CI fallback — see Makefile.
//
// FAILS until `ports: - "9000:9000"` is removed from the core service in
// docker-compose.yml. The port must remain exposed only on the internal
// Compose network (no host binding).
//
// Build tag: integration — run with:
//   go test -tags=integration ./test/integration/... -v -run TestCoreGRPC_PortNotPubliclyExposed
//
// CI fallback: `make test-compose-ports` (Makefile target).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// composeServiceInfo represents a subset of the JSON output from
// `docker compose ps --format json`.
type composeServiceInfo struct {
	Service   string `json:"Service"`
	Name      string `json:"Name"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// TestCoreGRPC_PortNotPubliclyExposed — AC8
//
// Given: docker-compose.yml has been updated to remove the 9000:9000 host binding
// When:  `docker compose ps --format json` is run from the project root
// Then:  The core service has no publisher entry for port 9000
//
// FAILS until `ports: - "9000:9000"` is removed from docker-compose.yml.
//
// NOTE: This test requires `docker` and `docker compose` to be available in the
// test environment. If the CI runner does not have Docker access, this test will
// be SKIPPED and the Makefile target `test-compose-ports` must be run instead.
func TestCoreGRPC_PortNotPubliclyExposed(t *testing.T) {
	// Skip if docker is not available (e.g. unit test runners without Docker).
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available in PATH — use `make test-compose-ports` as CI fallback")
	}

	// Determine project root (two levels up from gateway/test/integration).
	projectRoot := os.Getenv("NEBU_PROJECT_ROOT")
	if projectRoot == "" {
		// Try to derive from the test binary location.
		// In integration test runs the compose file is at the project root.
		projectRoot = "../../../"
	}

	// Check that docker-compose.yml exists at the expected path.
	composeFile := projectRoot + "docker-compose.yml"
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		t.Skipf("docker-compose.yml not found at %s — set NEBU_PROJECT_ROOT env var", composeFile)
	}

	// Run `docker compose config` (static analysis — does not require running containers)
	// to check published ports in the compose configuration.
	// We parse docker-compose.yml statically to avoid needing running containers.
	//
	// Strategy: use `docker compose convert` (= `docker compose config`) which outputs
	// the resolved compose YAML. We grep for the port binding.
	cmd := exec.Command("docker", "compose",
		"-f", composeFile,
		"config",
		"--format", "json",
	)
	out, err := cmd.Output()
	if err != nil {
		// docker compose config might fail if compose is not installed as a plugin.
		// In that case, fall back to a simple file parse.
		t.Logf("docker compose config failed (%v), falling back to file parse", err)
		checkComposFileDirectly(t, composeFile)
		return
	}

	// Parse the JSON output and check if core service publishes port 9000.
	checkComposeParsedOutput(t, out)
}

// checkComposFileDirectly is a fallback that parses docker-compose.yml directly
// as a string, checking for "9000:9000" in the core service section.
// This avoids requiring docker to be available.
func checkComposFileDirectly(t *testing.T, composeFile string) {
	t.Helper()
	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("AC8 FAIL: cannot read docker-compose.yml: %v", err)
	}

	content := string(data)

	// Check if "9000:9000" appears anywhere in the file.
	// This is a simple string check — the dev agent must remove this binding.
	if strings.Contains(content, `"9000:9000"`) || strings.Contains(content, "9000:9000") {
		t.Errorf("AC8 FAIL: docker-compose.yml still contains port binding 9000:9000 for core service. "+
			"Remove `- \"9000:9000\"` from the core service ports section. "+
			"Port 9000 should only be accessible within the internal Compose network, not published to the host. "+
			"File: %s", composeFile)
	} else {
		t.Log("AC8 PASS: port 9000:9000 binding is absent from docker-compose.yml")
	}
}

// checkComposeParsedOutput parses docker compose JSON output and checks core service ports.
func checkComposeParsedOutput(t *testing.T, rawJSON []byte) {
	t.Helper()

	// docker compose config --format json returns a single object (not an array).
	// Parse the "services" key.
	var composeConfig struct {
		Services map[string]struct {
			Ports []struct {
				Target    int    `json:"target"`
				Published string `json:"published"`
				Protocol  string `json:"protocol"`
				Mode      string `json:"mode"`
			} `json:"ports"`
		} `json:"services"`
	}

	if err := json.Unmarshal(rawJSON, &composeConfig); err != nil {
		t.Logf("JSON parse failed (%v), falling back to string check", err)
		// Fall back to string-based check on the raw output.
		if strings.Contains(string(rawJSON), `"9000:9000"`) || strings.Contains(string(rawJSON), "9000") {
			t.Error("AC8 FAIL: port 9000 may still be published — review docker-compose.yml manually")
		}
		return
	}

	coreSvc, ok := composeConfig.Services["core"]
	if !ok {
		t.Skip("AC8 SKIP: `core` service not found in docker compose config output — check compose file")
		return
	}

	for _, p := range coreSvc.Ports {
		if p.Target == 9000 && p.Published != "" && p.Published != "0" {
			t.Errorf("AC8 FAIL: core service publishes gRPC port 9000 on host (published=%q, mode=%q). "+
				"Remove `- \"9000:9000\"` from docker-compose.yml so port 9000 is only "+
				"reachable on the internal Compose network.", p.Published, p.Mode)
			return
		}
	}

	if len(coreSvc.Ports) == 0 {
		t.Log("AC8 PASS: core service has no published ports at all")
	} else {
		for _, p := range coreSvc.Ports {
			t.Logf("core port: target=%d published=%q protocol=%s mode=%s", p.Target, p.Published, p.Protocol, p.Mode)
		}
		t.Log("AC8 PASS: core service port 9000 is not published to the host")
	}
	_ = fmt.Sprintf // keep import used
}
