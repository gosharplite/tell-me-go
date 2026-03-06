// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
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
