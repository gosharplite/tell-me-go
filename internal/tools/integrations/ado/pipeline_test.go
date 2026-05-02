// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListPipelines(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name       string
		mockStatus int
		mockBody   string
		args       map[string]interface{}
		wantError  bool
		wantPipes  int
		wantNames  []string
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
			wantPipes: 1,
			wantNames: []string{"my-pipeline"},
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
			wantPipes: 0,
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			if tt.mockStatus != 0 {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					_, _ = w.Write([]byte(tt.mockBody))
				}))
				t.Cleanup(server.Close)
			}

			var opts []AdoOption
			if server != nil {
				opts = append(opts, WithBaseURL(server.URL))
			}
			opts = append(opts, WithToken("test-pat"))
			m := NewADOManager(sm, opts...)

			ctx := context.Background()
			pipelines, err := m.ListPipelines(ctx, tt.args)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Len(t, pipelines, tt.wantPipes)
				for i, name := range tt.wantNames {
					assert.Equal(t, name, pipelines[i].Name)
				}
			}
		})
	}
}

func TestFormatPipelineList(t *testing.T) {
	tests := []struct {
		name      string
		pipelines []adoPipeline
		want      string
	}{
		{
			name:      "Empty list returns sentinel",
			pipelines: nil,
			want:      "No pipelines found.",
		},
		{
			name:      "Single pipeline",
			pipelines: []adoPipeline{{Id: 1, Name: "my-pipeline"}},
			want:      "Found 1 pipelines:\n- [1] my-pipeline\n",
		},
		{
			name: "Multiple pipelines",
			pipelines: []adoPipeline{
				{Id: 1, Name: "a"},
				{Id: 2, Name: "b"},
			},
			want: "Found 2 pipelines:\n- [1] a\n- [2] b\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			formatter := NewPipelineFormatter()
			got := formatter.FormatPipelineList(tt.pipelines)
			assert.Equal(t, tt.want, got)
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			formatter := NewPipelineFormatter()
			if got := formatter.FormatBranchRef(tt.input); got != tt.want {
				t.Errorf("FormatBranchRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAdoCreatePipeline_WithVariables(t *testing.T) {
	server := setupMockPipelineServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req adoCreatePipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		assertVariable(t, req.Configuration.Variables, "x-api-key", "secret-key", true, true)

		// Verify variable groups are inside configuration
		if len(req.Configuration.VariableGroups) != 1 || req.Configuration.VariableGroups[0].ID != 456 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 789}`))
	})
	t.Cleanup(server.Close)

	m, ctx := setupADOManager(t, server.URL, true)
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

	result, err := m.CreatePipeline(ctx, args)
	require.NoError(t, err)
	assert.False(t, result.AlreadyExisted)
	assert.False(t, result.Cancelled)
	assert.Equal(t, 789, result.PipelineID)
	assert.Equal(t, "new-pipeline", result.Name)
}

type mockConfirmer struct {
	approved bool
}

func (m *mockConfirmer) Confirm(ctx context.Context, message string) (bool, error) {
	return m.approved, nil
}

func TestAdoCreatePipeline_WithOverrideControl(t *testing.T) {
	server := setupMockPipelineServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req adoCreatePipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		assertVariable(t, req.Configuration.Variables, "x-api-key", "secret-key", true, false)
		assertVariable(t, req.Configuration.Variables, "debug-mode", "true", false, true)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 888}`))
	})
	t.Cleanup(server.Close)

	m, ctx := setupADOManager(t, server.URL, true)
	args := map[string]interface{}{
		"organization":  "myorg",
		"project":       "myproj",
		"name":          "locked-pipeline",
		"repository_id": "repo-uuid",
		"yaml_path":     "/dev-3/main.yaml",
		"variables": map[string]interface{}{
			"x-api-key": map[string]interface{}{
				"value":         "secret-key",
				"isSecret":      true,
				"allowOverride": false,
			},
			"debug-mode": map[string]interface{}{
				"value":    "true",
				"isSecret": false,
			},
		},
	}

	result, err := m.CreatePipeline(ctx, args)
	require.NoError(t, err)
	assert.False(t, result.AlreadyExisted)
	assert.False(t, result.Cancelled)
	assert.Equal(t, 888, result.PipelineID)
	assert.Equal(t, "locked-pipeline", result.Name)
}

func setupMockPipelineServer(t *testing.T, postHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_apis/pipelines") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"count": 0, "value": []}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/_apis/pipelines") {
			postHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func setupADOManager(t *testing.T, baseURL string, approved bool) (*AdoManager, context.Context) {
	t.Helper()
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mc := &mockConfirmer{approved: approved}
	m := NewADOManager(mc, WithBaseURL(baseURL), WithToken("test-pat"))
	return m, context.Background()
}

func assertVariable(t *testing.T, vars map[string]adoVariable, name string, value string, isSecret bool, allowOverride bool) {
	t.Helper()
	v, ok := vars[name]
	require.True(t, ok, "variable %s missing", name)
	assert.Equal(t, value, v.Value)
	assert.Equal(t, isSecret, v.IsSecret)
	require.NotNil(t, v.AllowOverride)
	assert.Equal(t, allowOverride, *v.AllowOverride)
	if v.IsSettableAtQueueTime != nil {
		assert.Equal(t, allowOverride, *v.IsSettableAtQueueTime)
	}
}

func TestGetPipelineDefinition(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/_apis/pipelines/123")
		assert.Equal(t, "7.1-preview.1", r.URL.Query().Get("api-version"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": 123,
			"name": "test-pipeline",
			"configuration": {
				"type": "yaml",
				"path": "/azure-pipeline.yaml",
				"variables": {
					"secret-var": {
						"value": null,
						"isSecret": true,
						"isSettableAtQueueTime": false
					}
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"pipeline_id":  123,
	}

	def, err := m.GetPipelineDefinition(ctx, args)
	require.NoError(t, err)

	// def is the decoded JSON, type-assert and index into tree
	defMap, ok := def.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, float64(123), defMap["id"])
	assert.Equal(t, "test-pipeline", defMap["name"])

	config := defMap["configuration"].(map[string]interface{})
	vars := config["variables"].(map[string]interface{})
	secretVar := vars["secret-var"].(map[string]interface{})

	assert.True(t, secretVar["isSecret"].(bool))
	assert.False(t, secretVar["isSettableAtQueueTime"].(bool))
}

func TestAdoUpdateBuildDefinitionVariables(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mc := &mockConfirmer{approved: true}

	var getCalled, putCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/_apis/build/definitions/123")
		assert.Equal(t, "7.1", r.URL.Query().Get("api-version"))

		if r.Method == http.MethodGet {
			getCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": 123,
				"name": "test-build",
				"variables": {
					"old-var": { "value": "old", "isSecret": false, "allowOverride": true }
				}
			}`))
			return
		}

		if r.Method == http.MethodPut {
			putCalled = true
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify modified variables
			vars := body["variables"].(map[string]interface{})
			if len(vars) != 2 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			newVar := vars["new-var"].(map[string]interface{})
			if newVar["value"] != "new" || newVar["allowOverride"] != false {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(mc, WithBaseURL(server.URL), WithToken("test-pat"))

	ctx := context.Background()
	args := map[string]interface{}{
		"organization":  "myorg",
		"project":       "myproj",
		"definition_id": 123,
		"variables": map[string]interface{}{
			"new-var": map[string]interface{}{
				"value":         "new",
				"isSecret":      false,
				"allowOverride": false,
			},
		},
	}

	result, err := m.UpdateBuildDefinitionVariables(ctx, args)
	require.NoError(t, err)
	assert.False(t, result.Cancelled)
	assert.Equal(t, 123, result.DefinitionID)
	assert.True(t, getCalled)
	assert.True(t, putCalled)
}

func TestStreamRegexFilter_Truncation(t *testing.T) {
	tests := []struct {
		name           string
		inputLines     int
		maxLines       int
		wantTruncated  bool
		wantMatchCount int
	}{
		{
			name:           "Happy Path: No truncation",
			inputLines:     10,
			maxLines:       20,
			wantTruncated:  false,
			wantMatchCount: 10,
		},
		{
			name:           "Edge Case: Exact limit",
			inputLines:     10,
			maxLines:       10,
			wantTruncated:  false,
			wantMatchCount: 10,
		},
		{
			name:           "Truncation: Over limit",
			inputLines:     15,
			maxLines:       10,
			wantTruncated:  true,
			wantMatchCount: 10,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var input strings.Builder
			for i := 0; i < tt.inputLines; i++ {
				input.WriteString("match line\n")
			}

			res, err := streamRegexFilter(context.Background(), strings.NewReader(input.String()), "match", logFilterOptions{MaxLines: tt.maxLines}, nil)
			require.NoError(t, err)

			assert.Equal(t, tt.wantTruncated, res.Truncated)
			assert.Equal(t, tt.wantMatchCount, res.TotalLines)

			// Count matching lines in output
			matches := strings.Count(res.Content, "match line")
			assert.Equal(t, tt.wantMatchCount, matches)
		})
	}
}
