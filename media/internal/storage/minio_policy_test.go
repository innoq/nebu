package storage_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// iamPolicy mirrors the relevant subset of an AWS/MinIO IAM policy document.
type iamPolicy struct {
	Version   string         `json:"Version"`
	Statement []iamStatement `json:"Statement"`
}

type iamStatement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

// policyFilePath returns the absolute path to dev/minio/nebu-app-policy.json.
// Strategy: Walk up from the current working directory until we find the
// marker file dev/minio/nebu-app-policy.json (or go.work / .git).
// In Docker builds the test runs from /app (the media module root), so we
// walk upward: /app → /app/.. until the marker is found (max 5 levels).
func policyFilePath(t *testing.T) string {
	t.Helper()

	// First, try the NEBU_PROJECT_ROOT env var (useful for CI overrides).
	if root := os.Getenv("NEBU_PROJECT_ROOT"); root != "" {
		p := filepath.Join(root, "dev", "minio", "nebu-app-policy.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Walk up from cwd.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}

	dir := cwd
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "dev", "minio", "nebu-app-policy.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fallback using runtime.Caller: file is at <repo>/media/internal/storage/minio_policy_test.go
	_, file, _, ok := runtime.Caller(0)
	if ok {
		// Climb: storage → internal → media → repo-root
		candidate := filepath.Join(filepath.Dir(file), "..", "..", "..", "dev", "minio", "nebu-app-policy.json")
		candidate = filepath.Clean(candidate)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	t.Fatalf("could not locate dev/minio/nebu-app-policy.json from cwd=%s", cwd)
	return ""
}

func loadPolicy(t *testing.T) iamPolicy {
	t.Helper()
	path := policyFilePath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	var p iamPolicy
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("invalid JSON in %s: %v", path, err)
	}
	return p
}

// AT-4 — TestMinIOPolicy_NoPublicAccess
// RED: policy file must exist and satisfy least-privilege requirements.
// Verifies that only s3:PutObject and s3:GetObject are allowed,
// and that s3:DeleteObject, s3:* and s3:ListBucket are absent.
func TestMinIOPolicy_NoPublicAccess(t *testing.T) {
	policy := loadPolicy(t)

	if len(policy.Statement) == 0 {
		t.Fatal("policy has no statements")
	}

	forbidden := []string{"s3:DeleteObject", "s3:*", "s3:ListBucket"}

	for i, stmt := range policy.Statement {
		// Check required actions are present in at least one Allow statement.
		if stmt.Effect != "Allow" {
			continue
		}

		actionSet := make(map[string]bool, len(stmt.Action))
		for _, a := range stmt.Action {
			actionSet[a] = true
		}

		for _, bad := range forbidden {
			if actionSet[bad] {
				t.Errorf("statement[%d]: forbidden action %q is present", i, bad)
			}
		}
	}

	// Verify at least one Allow statement grants PutObject and GetObject.
	hasPut := false
	hasGet := false
	for _, stmt := range policy.Statement {
		if stmt.Effect != "Allow" {
			continue
		}
		for _, a := range stmt.Action {
			if a == "s3:PutObject" {
				hasPut = true
			}
			if a == "s3:GetObject" {
				hasGet = true
			}
		}
	}
	if !hasPut {
		t.Error("policy must allow s3:PutObject but it does not")
	}
	if !hasGet {
		t.Error("policy must allow s3:GetObject but it does not")
	}
}

// AT-5 — TestMinIOPolicy_ResourceScope
// RED: policy file must exist and scope resources to nebu-media/* only.
// Verifies all resource ARNs are of the form arn:aws:s3:::nebu-media/*
// and no wildcard bucket-level ARN (arn:aws:s3:::*) is present.
func TestMinIOPolicy_ResourceScope(t *testing.T) {
	policy := loadPolicy(t)

	for i, stmt := range policy.Statement {
		for j, resource := range stmt.Resource {
			label := fmt.Sprintf("statement[%d].Resource[%d] = %q", i, j, resource)

			// Must be scoped to nebu-media/* — no bare bucket or wildcard
			if resource == "arn:aws:s3:::*" {
				t.Errorf("%s: wildcard bucket-level ARN not allowed", label)
			}
			if resource == "arn:aws:s3:::nebu-media" {
				t.Errorf("%s: bucket-level ARN without /* not allowed — use nebu-media/*", label)
			}
			if !strings.HasPrefix(resource, "arn:aws:s3:::nebu-media/") {
				t.Errorf("%s: expected prefix arn:aws:s3:::nebu-media/", label)
			}
		}
	}
}
