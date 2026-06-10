// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"strings"
	"testing"
)

func TestFormatBranchPolicies(t *testing.T) {
	m := &AdoManager{}

	tests := []struct {
		name           string
		branchName     string
		repositoryName string
		policyConfigs  []adoPolicyConfig
		targetRepoId   string
		assertFn       func(t *testing.T, result string)
	}{
		{
			name:           "empty configs",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs:  []adoPolicyConfig{},
			targetRepoId:   "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "No active policies found") {
					t.Errorf("expected 'No active policies found', got: %s", result)
				}
			},
		},
		{
			name:           "disabled config",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  false,
					IsBlocking: true,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "No active policies found") {
					t.Errorf("expected 'No active policies found', got: %s", result)
				}
			},
		},
		{
			name:           "non-matching branch",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/develop"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "No active policies found") {
					t.Errorf("expected 'No active policies found', got: %s", result)
				}
			},
		},
		{
			name:           "single enabled matching",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "- Type: Build") {
					t.Errorf("expected '- Type: Build', got: %s", result)
				}
				if !strings.Contains(result, "Status: Enabled") {
					t.Errorf("expected 'Status: Enabled', got: %s", result)
				}
				if !strings.Contains(result, "Branch Policies for main in myrepo") {
					t.Errorf("expected header, got: %s", result)
				}
			},
		},
		{
			name:           "blocking policy",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: true,
					Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, " [REQUIRED]") {
					t.Errorf("expected ' [REQUIRED]' in output, got: %s", result)
				}
			},
		},
		{
			name:           "non-blocking policy",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if strings.Contains(result, "[REQUIRED]") {
					t.Errorf("did NOT expect '[REQUIRED]' in output, got: %s", result)
				}
			},
		},
		{
			name:           "branch name normalization",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				// The function should internally normalize "main" to "refs/heads/main"
				if !strings.Contains(result, "- Type: Build") {
					t.Errorf("expected policy to match after branch normalization, got: %s", result)
				}
			},
		},
		{
			name:           "branch already has refs/heads prefix",
			branchName:     "refs/heads/main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if !strings.Contains(result, "- Type: Build") {
					t.Errorf("expected policy to match with full ref, got: %s", result)
				}
			},
		},
		{
			name:           "settings with scope key skipped",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings: map[string]interface{}{
						"scope": []interface{}{
							map[string]interface{}{"repositoryId": "repo-guid", "refName": "refs/heads/main"},
						},
						"minimumApproverCount": float64(2),
						"buildDefinitionId":    float64(42),
					},
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if strings.Contains(result, "scope:") || strings.Contains(result, "Scope:") {
					t.Errorf("expected 'scope' to be skipped in settings output, got: %s", result)
				}
				if !strings.Contains(result, "Minimum Approver Count: 2") {
					t.Errorf("expected 'Minimum Approver Count: 2', got: %s", result)
				}
				if !strings.Contains(result, "Build Definition ID: 42") {
					t.Errorf("expected 'Build Definition ID: 42', got: %s", result)
				}
			},
		},
		{
			name:           "multiple policies mixed",
			branchName:     "main",
			repositoryName: "myrepo",
			policyConfigs: []adoPolicyConfig{
				{
					IsEnabled:  false, // disabled - should be skipped
					IsBlocking: true,
					Type:       adoPolicyType{DisplayName: "DisabledPolicy"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
				{
					IsEnabled:  true,
					IsBlocking: true,
					Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "Build"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
				},
				{
					IsEnabled:  true,
					IsBlocking: false,
					Type:       adoPolicyType{DisplayName: "WrongBranch"},
					Settings:   newMatchingScope("repo-guid", "refs/heads/develop"),
				},
			},
			targetRepoId: "repo-guid",
			assertFn: func(t *testing.T, result string) {
				if strings.Contains(result, "DisabledPolicy") {
					t.Errorf("disabled policy should not appear, got: %s", result)
				}
				if strings.Contains(result, "WrongBranch") {
					t.Errorf("non-matching branch policy should not appear, got: %s", result)
				}
				if !strings.Contains(result, "RequiredReviewer") {
					t.Errorf("expected RequiredReviewer in output, got: %s", result)
				}
				if !strings.Contains(result, "Build") {
					t.Errorf("expected Build in output, got: %s", result)
				}
				// Count policy blocks: each policy has "- Type:" line
				count := strings.Count(result, "- Type:")
				if count != 2 {
					t.Errorf("expected 2 policy blocks, got %d: %s", count, result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.formatBranchPolicies(tt.branchName, tt.repositoryName, tt.policyConfigs, tt.targetRepoId)
			tt.assertFn(t, result)
		})
	}
}

// newMatchingScope creates a Settings map with a scope entry matching the given repoId and refName.
func newMatchingScope(repoId, refName string) map[string]interface{} {
	return map[string]interface{}{
		"scope": []interface{}{
			map[string]interface{}{
				"repositoryId": repoId,
				"refName":      refName,
			},
		},
	}
}
