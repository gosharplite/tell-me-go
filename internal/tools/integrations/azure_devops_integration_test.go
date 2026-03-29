//go:build integration

package integrations

import (
	"context"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
)

// TestAdoManager_LiveNetwork_Integration performs a real network call to Azure DevOps.
// This test is isolated by the 'integration' build tag and requires a valid AZURE_PAT_ALL.
func TestAdoManager_LiveNetwork_Integration(t *testing.T) {
	// 1. Skip if credentials are not provided (defense in depth)
	pat := os.Getenv("AZURE_PAT_ALL")
	if pat == "" {
		t.Skip("Skipping live integration test: AZURE_PAT_ALL is not set")
	}

	// 2. Initialize with defaults (hits real dev.azure.com by default)
	sm := security.NewSecurityManager(nil)
	m := newADOManager(sm, withToken(pat))

	// 3. Execute real network call (Example: listing PRs in a public or accessible repo)
	// Note: We use a placeholder organization/project/repo that would likely exist or fail gracefully.
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "microsoft", // Public org
		"project":      "vscode",    // Public project
		"repository":   "vscode",    // Public repo
	}

	// We don't expect this to necessarily succeed without a real PAT that has access,
	// but we want to verify it actually tries to hit the network.
	result, err := m.adoListPullRequests(ctx, args, nil)

	// If it fails with unauthorized, it means it reached the server!
	if err != nil {
		assert.Contains(t, err.Error(), "unauthorized", "Expected unauthorized error if PAT is invalid/limited, but got: %v", err)
	} else {
		assert.NotEmpty(t, result.Text)
	}
}
