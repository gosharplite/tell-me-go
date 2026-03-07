// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdoListPipelines(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	tests := []struct {
		name       string
		mockStatus int
		mockBody   string
		args       map[string]interface{}
		wantError  bool
		wantText   string
		errMsg     string
	}{
		{
			name:       "Success returns pipelines",
			mockStatus: http.StatusOK,
			mockBody:   `{"count": 1, "value": [{"id": 1, "name": "my-pipeline"}]}`,
			args: map[string]interface{}{
				"organization": "myorg",
				"project":      "myproj",
			},
			wantError: false,
			wantText:  "Found 1 pipelines:\n- [1] my-pipeline\n",
		},
		{
			name:       "Empty list",
			mockStatus: http.StatusOK,
			mockBody:   `{"count": 0, "value": []}`,
			args: map[string]interface{}{
				"organization": "myorg",
				"project":      "myproj",
			},
			wantError: false,
			wantText:  "No pipelines found.",
		},
		{
			name:       "API returns 500 Error",
			mockStatus: http.StatusInternalServerError,
			mockBody:   `{"error": "Internal Server Error"}`,
			args: map[string]interface{}{
				"organization": "myorg",
				"project":      "myproj",
			},
			wantError: true,
			errMsg:    "returned status: 500",
		},
		{
			name:       "Malformed JSON response",
			mockStatus: http.StatusOK,
			mockBody:   `{invalid-json}`,
			args: map[string]interface{}{
				"organization": "myorg",
				"project":      "myproj",
			},
			wantError: true,
			errMsg:    "failed to decode response",
		},
		{
			name: "Missing required parameters",
			args: map[string]interface{}{
				"organization": "myorg",
			},
			wantError: true,
			errMsg:    "organization and project are required",
		},
		{
			name: "Invalid argument type",
			args: map[string]interface{}{
				"organization": 123,
			},
			wantError: true,
			errMsg:    "json: cannot unmarshal number into Go struct field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.mockStatus != 0 {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					_, _ = w.Write([]byte(tt.mockBody))
				}))
				defer server.Close()
			}

			var opts []ADOOption
			if server != nil {
				opts = append(opts, WithBaseURL(server.URL))
			}
			opts = append(opts, WithToken("test-pat"))
			m := NewADOManager(sm, opts...)

			ctx := context.Background()
			result, err := m.adoListPipelines(ctx, tt.args)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantText, result.Text)
			}
		})
	}
}

func TestFormatBranchRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"Empty defaults to main", "", "refs/heads/main"},
		{"Already formatted ref", "refs/pull/1/merge", "refs/pull/1/merge"},
		{"Version tag", "v2.0.1", "refs/tags/v2.0.1"},
		{"Standard branch", "feature-x", "refs/heads/feature-x"},
		{"Short tag (no digits)", "v", "refs/heads/v"},
		{"Not a version tag", "v-beta", "refs/heads/v-beta"},
		{"Another standard branch", "main", "refs/heads/main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBranchRef(tt.input); got != tt.want {
				t.Errorf("formatBranchRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAdoCreatePipeline_WithVariables(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mc := &mockConfirmer{approved: true}

	// Mock server to catch the POST request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_apis/pipelines") {
			// Idempotency check
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"count": 0, "value": []}`))
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/_apis/pipelines") {
			var req adoCreatePipelineRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify variables are inside configuration
			if len(req.Configuration.Variables) != 1 || req.Configuration.Variables["x-api-key"].Value != "secret-key" || !req.Configuration.Variables["x-api-key"].IsSecret {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify allowOverride is true
			if !req.Configuration.Variables["x-api-key"].AllowOverride {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify variable groups are inside configuration
			if len(req.Configuration.VariableGroups) != 1 || req.Configuration.VariableGroups[0].ID != 456 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 789}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	m := NewADOManager(mc, WithBaseURL(server.URL), WithToken("test-pat"))

	ctx := context.Background()
	args := map[string]interface{}{
		"organization":    "myorg",
		"project":         "myproj",
		"name":            "new-pipeline",
		"repository_id":   "repo-uuid",
		"yaml_path":       "/dev-3/main.yaml",
		"variable_groups": []int{456},
		"variables": map[string]interface{}{
			"x-api-key": map[string]interface{}{
				"value":    "secret-key",
				"isSecret": true,
			},
		},
	}

	result, err := m.adoCreatePipeline(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result.Text, "Successfully created pipeline 'new-pipeline' with ID: 789")
}

type mockConfirmer struct {
	approved bool
}

func (m *mockConfirmer) Confirm(ctx context.Context, message string) (bool, error) {
	return m.approved, nil
}
