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
			errMsg:    "decoding response",
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
			pipelines, err := m.listPipelines(ctx, tt.args)

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
			formatter := newPipelineFormatter()
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
			formatter := newPipelineFormatter()
			if got := formatter.FormatBranchRef(tt.input); got != tt.want {
				t.Errorf("FormatBranchRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseGetBuildChangesArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantTop     int
	}{
		{
			name:    "Success with explicit top",
			args:    map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "top": 20},
			wantErr: false,
			wantTop: 20,
		},
		{
			name:    "Top zero defaults to 50",
			args:    map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "top": 0},
			wantErr: false,
			wantTop: 50,
		},
		{
			name:    "Top negative defaults to 50",
			args:    map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "top": -5},
			wantErr: false,
			wantTop: 50,
		},
		{
			name:    "Top exceeds 1000 clamps to 1000",
			args:    map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "top": 2000},
			wantErr: false,
			wantTop: 1000,
		},
		{
			name:    "Top at boundary 1000",
			args:    map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "top": 1000},
			wantErr: false,
			wantTop: 1000,
		},
		{
			name:        "Missing organization",
			args:        map[string]interface{}{"project": "p", "build_id": 1},
			wantErr:     true,
			errContains: "organization, project, and build_id are required",
		},
		{
			name:        "Missing project",
			args:        map[string]interface{}{"organization": "o", "build_id": 1},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Missing build_id",
			args:        map[string]interface{}{"organization": "o", "project": "p"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Build_id is zero",
			args:        map[string]interface{}{"organization": "o", "project": "p", "build_id": 0},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Invalid type for organization",
			args:        map[string]interface{}{"organization": 123, "project": "p", "build_id": 1},
			wantErr:     true,
			errContains: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parseGetBuildChangesArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantTop, params.Top)
		})
	}
}

func TestParseListPipelineRunsArgs(t *testing.T) {
	tests := []struct {
		name             string
		args             map[string]interface{}
		wantErr          bool
		errContains      string
		wantPipelineID   int
		wantPipelineName string
		wantOriginalTop  int
		wantFetchTop     int
	}{
		{
			name:            "Success with pipeline_id",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 5},
			wantErr:         false,
			wantPipelineID:  5,
			wantOriginalTop: 10,
			wantFetchTop:    10,
		},
		{
			name:             "Success with pipeline_name",
			args:             map[string]interface{}{"organization": "o", "project": "p", "pipeline_name": "my-pipe"},
			wantErr:          false,
			wantPipelineID:   0,
			wantPipelineName: "my-pipe",
			wantOriginalTop:  10,
			wantFetchTop:     10,
		},
		{
			name:        "Neither pipeline_id nor pipeline_name",
			args:        map[string]interface{}{"organization": "o", "project": "p"},
			wantErr:     true,
			errContains: "either pipeline_id or pipeline_name must be provided",
		},
		{
			name:            "Top zero defaults to 10",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": 0},
			wantErr:         false,
			wantPipelineID:  1,
			wantOriginalTop: 10,
			wantFetchTop:    10,
		},
		{
			name:            "Top negative defaults to 10",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": -5},
			wantErr:         false,
			wantPipelineID:  1,
			wantOriginalTop: 10,
			wantFetchTop:    10,
		},
		{
			name:            "Top exceeds 1000 clamps to 1000",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": 2000},
			wantErr:         false,
			wantPipelineID:  1,
			wantOriginalTop: 1000,
			wantFetchTop:    1000,
		},
		{
			name:            "Top with repo filter sets FetchTop to 100",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": 20, "repository": "some-repo"},
			wantErr:         false,
			wantPipelineID:  1,
			wantOriginalTop: 20,
			wantFetchTop:    100,
		},
		{
			name:            "Top at boundary 1000",
			args:            map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": 1000},
			wantErr:         false,
			wantPipelineID:  1,
			wantOriginalTop: 1000,
			wantFetchTop:    1000,
		},
		{
			name:        "Missing organization",
			args:        map[string]interface{}{"project": "p", "pipeline_id": 1},
			wantErr:     true,
			errContains: "organization and project are required",
		},
		{
			name:        "Invalid type for organization",
			args:        map[string]interface{}{"organization": 123, "project": "p", "pipeline_id": 1},
			wantErr:     true,
			errContains: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parseListPipelineRunsArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPipelineID, params.PipelineId)
			assert.Equal(t, tt.wantPipelineName, params.PipelineName)
			assert.Equal(t, tt.wantOriginalTop, params.OriginalTop)
			assert.Equal(t, tt.wantFetchTop, params.FetchTop)
		})
	}
}

func TestParseCreatePipelineArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantName    string
	}{
		{
			name:     "Success",
			args:     map[string]interface{}{"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y"},
			wantErr:  false,
			wantName: "n",
		},
		{
			name:        "Missing organization",
			args:        map[string]interface{}{"project": "p", "name": "n", "repository_id": "r", "yaml_path": "y"},
			wantErr:     true,
			errContains: "organization, project, name, repository_id, and yaml_path are required",
		},
		{
			name:        "Missing project",
			args:        map[string]interface{}{"organization": "o", "name": "n", "repository_id": "r", "yaml_path": "y"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Missing name",
			args:        map[string]interface{}{"organization": "o", "project": "p", "repository_id": "r", "yaml_path": "y"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Missing repository_id",
			args:        map[string]interface{}{"organization": "o", "project": "p", "name": "n", "yaml_path": "y"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Missing yaml_path",
			args:        map[string]interface{}{"organization": "o", "project": "p", "name": "n", "repository_id": "r"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Invalid type for name",
			args:        map[string]interface{}{"organization": "o", "project": "p", "name": 123, "repository_id": "r", "yaml_path": "y"},
			wantErr:     true,
			errContains: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parseCreatePipelineArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantName, params.Name)
		})
	}
}

func TestParseUpdateBuildDefArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantDefID   int
	}{
		{
			name: "Success",
			args: map[string]interface{}{
				"organization":  "o",
				"project":       "p",
				"definition_id": 1,
				"variables":     map[string]interface{}{"KEY": map[string]interface{}{"value": "v"}},
			},
			wantErr:   false,
			wantDefID: 1,
		},
		{
			name: "Missing organization",
			args: map[string]interface{}{
				"project":       "p",
				"definition_id": 1,
				"variables":     map[string]interface{}{"K": map[string]interface{}{"value": "v"}},
			},
			wantErr:     true,
			errContains: "organization, project, definition_id, and non-empty variables are required",
		},
		{
			name: "Missing project",
			args: map[string]interface{}{
				"organization":  "o",
				"definition_id": 1,
				"variables":     map[string]interface{}{"K": map[string]interface{}{"value": "v"}},
			},
			wantErr:     true,
			errContains: "required",
		},
		{
			name: "Missing definition_id (zero)",
			args: map[string]interface{}{
				"organization": "o",
				"project":      "p",
				"variables":    map[string]interface{}{"K": map[string]interface{}{"value": "v"}},
			},
			wantErr:     true,
			errContains: "required",
		},
		{
			name: "Empty variables",
			args: map[string]interface{}{
				"organization":  "o",
				"project":       "p",
				"definition_id": 1,
				"variables":     map[string]interface{}{},
			},
			wantErr:     true,
			errContains: "required",
		},
		{
			name: "Invalid type for definition_id",
			args: map[string]interface{}{
				"organization":  "o",
				"project":       "p",
				"definition_id": "not-an-int",
				"variables":     map[string]interface{}{"K": map[string]interface{}{"value": "v"}},
			},
			wantErr:     true,
			errContains: "cannot unmarshal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parseUpdateBuildDefArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantDefID, params.DefinitionId)
		})
	}
}

func TestParseRunPipelineArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantErr     bool
		errContains string
		wantPipeID  int
		wantRefName string
		wantBranch  string
	}{
		{
			name: "Success",
			args: map[string]interface{}{
				"organization": "o",
				"project":      "p",
				"pipeline_id":  1,
				"branch":       "main",
				"_ref_name":    "refs/heads/main",
			},
			wantErr:     false,
			wantPipeID:  1,
			wantRefName: "refs/heads/main",
			wantBranch:  "main",
		},
		{
			name:        "Missing organization",
			args:        map[string]interface{}{"project": "p", "pipeline_id": 1},
			wantErr:     true,
			errContains: "organization, project, and pipeline_id are required",
		},
		{
			name:        "Missing project",
			args:        map[string]interface{}{"organization": "o", "pipeline_id": 1},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Pipeline_id is zero",
			args:        map[string]interface{}{"organization": "o", "project": "p"},
			wantErr:     true,
			errContains: "required",
		},
		{
			name:        "Invalid type for pipeline_id",
			args:        map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": "bad"},
			wantErr:     true,
			errContains: "cannot unmarshal",
		},
		{
			name: "RefName fallback from Branch",
			args: map[string]interface{}{
				"organization": "o",
				"project":      "p",
				"pipeline_id":  1,
				"branch":       "develop",
			},
			wantErr:     false,
			wantPipeID:  1,
			wantRefName: "develop",
			wantBranch:  "develop",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params, err := parseRunPipelineArgs(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPipeID, params.PipelineId)
			assert.Equal(t, tt.wantRefName, params.RefName)
			assert.Equal(t, tt.wantBranch, params.Branch)
		})
	}

	t.Run("Variables mapped to adoVariable", func(t *testing.T) {
		params, err := parseRunPipelineArgs(map[string]interface{}{
			"organization": "o",
			"project":      "p",
			"pipeline_id":  1,
			"variables":    map[string]string{"KEY": "VAL"},
		})
		assert.NoError(t, err)
		assert.NotNil(t, params.MappedVariables)
		v, ok := params.MappedVariables["KEY"]
		assert.True(t, ok)
		assert.Equal(t, "VAL", v.Value)
		assert.False(t, v.IsSecret)
	})
}

func TestBuildListPipelineRunsURL(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name            string
		baseURL         string
		org             string
		project         string
		pipelineID      int
		top             int
		wantErr         bool
		errContains     string
		wantURLContains []string
	}{
		{
			name:            "Success",
			baseURL:         "http://example.com",
			org:             "o",
			project:         "p",
			pipelineID:      1,
			top:             20,
			wantErr:         false,
			wantURLContains: []string{"definitions=1", "%24top=20", "api-version=7.1"},
		},
		{
			name:        "Malformed BaseURL causes parse error",
			baseURL:     "://invalid-url",
			org:         "o",
			project:     "p",
			pipelineID:  1,
			top:         10,
			wantErr:     true,
			errContains: "failed to parse base URL",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewADOManager(sm, WithBaseURL(tt.baseURL), WithToken("test-pat"))
			urlStr, err := m.buildListPipelineRunsURL(tt.org, tt.project, tt.pipelineID, tt.top)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			for _, want := range tt.wantURLContains {
				assert.Contains(t, urlStr, want)
			}
		})
	}
}

func TestBuildGetBuildChangesURL(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	// NOTE: Clamping of Top (≤0 → 50, >1000 → 1000) is tested in
	// TestParseGetBuildChangesArgs. buildGetBuildChangesURL trusts the
	// pre-clamped value and therefore does not re-clamp.

	tests := []struct {
		name            string
		baseURL         string
		org             string
		project         string
		buildID         int
		top             int
		wantErr         bool
		errContains     string
		wantURLContains []string
	}{
		{
			name:    "Success",
			baseURL: "http://example.com",
			org:     "o",
			project: "p",
			buildID: 1,
			top:     20,
			wantErr: false,
			wantURLContains: []string{
				"/_apis/build/builds/1/changes",
				"%24top=20",
				"api-version=7.0",
			},
		},
		{
			name:        "Malformed BaseURL causes parse error",
			baseURL:     "://invalid-url",
			org:         "o",
			project:     "p",
			buildID:     1,
			top:         10,
			wantErr:     true,
			errContains: "failed to parse base URL",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewADOManager(sm, WithBaseURL(tt.baseURL), WithToken("test-pat"))
			params := adoGetBuildChangesParams{
				Organization: tt.org,
				Project:      tt.project,
				BuildId:      tt.buildID,
				Top:          tt.top,
			}
			urlStr, err := m.buildGetBuildChangesURL(params)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			for _, want := range tt.wantURLContains {
				assert.Contains(t, urlStr, want)
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

	result, err := m.createPipeline(ctx, args)
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

	result, err := m.createPipeline(ctx, args)
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

// TestExecuteCreatePipeline_MarshalEdgeCases verifies the JSON body shape produced
// by executeCreatePipeline when optional fields are at their zero values. This
// directly exercises the json.Marshal call in executeCreatePipeline and confirms
// that the serialized payload is correct for edge-case inputs.
//
// NOTE: The json.Marshal error branch in executeCreatePipeline (pipeline_crud.go:201-203)
// is unreachable with current types (all fields are JSON-safe: string, int, bool, *bool).
// This is defense-in-depth dead code tracked by issue #1057.
func TestExecuteCreatePipeline_MarshalEdgeCases(t *testing.T) {
	t.Run("EmptyVariables", func(t *testing.T) {
		var capturedReq adoCreatePipelineRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/_apis/pipelines") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"count":0,"value":[]}`))
				return
			}
			if r.Method == http.MethodPost {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedReq))
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id": 999}`))
				return
			}
		}))
		t.Cleanup(server.Close)
		m, ctx := setupADOManager(t, server.URL, true)

		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"name":          "min-pipe",
			"repository_id": "repo-uuid",
			"yaml_path":     "/azure-pipelines.yml",
		}
		result, err := m.createPipeline(ctx, args)
		require.NoError(t, err)
		assert.Equal(t, 999, result.PipelineID)
		assert.Empty(t, capturedReq.Configuration.Variables)
		assert.Empty(t, capturedReq.Configuration.VariableGroups)
	})

	t.Run("EmptyVariableGroups", func(t *testing.T) {
		var capturedReq adoCreatePipelineRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/_apis/pipelines") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"count":0,"value":[]}`))
				return
			}
			if r.Method == http.MethodPost {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedReq))
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id": 1000}`))
				return
			}
		}))
		t.Cleanup(server.Close)
		m, ctx := setupADOManager(t, server.URL, true)

		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"name":          "min-pipe",
			"repository_id": "repo-uuid",
			"yaml_path":     "/azure-pipelines.yml",
			"variables": map[string]interface{}{
				"DEBUG": map[string]interface{}{
					"value":    "1",
					"isSecret": false,
				},
			},
		}
		result, err := m.createPipeline(ctx, args)
		require.NoError(t, err)
		assert.Equal(t, 1000, result.PipelineID)
		// Variables present but no variable groups
		assert.Len(t, capturedReq.Configuration.Variables, 1)
		assert.Empty(t, capturedReq.Configuration.VariableGroups)
	})

	t.Run("MinimalPayload", func(t *testing.T) {
		var capturedReq adoCreatePipelineRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/_apis/pipelines") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"count":0,"value":[]}`))
				return
			}
			if r.Method == http.MethodPost {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedReq))
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id": 1001}`))
				return
			}
		}))
		t.Cleanup(server.Close)
		m, ctx := setupADOManager(t, server.URL, true)

		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"name":          "min-pipe",
			"repository_id": "repo-uuid",
			"yaml_path":     "/azure-pipelines.yml",
		}
		result, err := m.createPipeline(ctx, args)
		require.NoError(t, err)
		assert.Equal(t, 1001, result.PipelineID)
		assert.Equal(t, "min-pipe", capturedReq.Name)
		assert.Equal(t, "yaml", capturedReq.Configuration.Type)
		assert.Empty(t, capturedReq.Configuration.Variables)
		assert.Empty(t, capturedReq.Configuration.VariableGroups)
	})
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

func TestBuildVariablesUpdatePayload(t *testing.T) {
	allowTrue := true
	allowFalse := false

	tests := []struct {
		name        string
		existingDef map[string]interface{}
		inputVars   map[string]adoVariable
		assertions  func(t *testing.T, body []byte, err error)
	}{
		{
			name:        "Creates variables key when absent",
			existingDef: map[string]interface{}{},
			inputVars:   map[string]adoVariable{"k": {Value: "v", IsSecret: false}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
				vars, ok := result["variables"].(map[string]interface{})
				require.True(t, ok, "variables key should be present")
				k, ok := vars["k"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "v", k["value"])
			},
		},
		{
			name: "Merges with existing variables",
			existingDef: map[string]interface{}{
				"variables": map[string]interface{}{
					"old": map[string]interface{}{
						"value":         "old",
						"isSecret":      true,
						"allowOverride": true,
					},
				},
			},
			inputVars: map[string]adoVariable{"new": {Value: "new", IsSecret: false}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
				vars, ok := result["variables"].(map[string]interface{})
				require.True(t, ok)
				// Old variable unchanged
				old, ok := vars["old"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "old", old["value"])
				assert.Equal(t, true, old["isSecret"])
				assert.Equal(t, true, old["allowOverride"])
				// New variable present
				newVar, ok := vars["new"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "new", newVar["value"])
			},
		},
		{
			name: "Omits allowOverride when nil",
			existingDef: map[string]interface{}{
				"variables": map[string]interface{}{},
			},
			inputVars: map[string]adoVariable{"k": {Value: "v"}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
				vars, ok := result["variables"].(map[string]interface{})
				require.True(t, ok)
				k, ok := vars["k"].(map[string]interface{})
				require.True(t, ok)
				_, hasOverride := k["allowOverride"]
				assert.False(t, hasOverride, "allowOverride should be absent when nil")
			},
		},
		{
			name:        "Includes allowOverride when true",
			existingDef: map[string]interface{}{},
			inputVars:   map[string]adoVariable{"k": {Value: "v", AllowOverride: &allowTrue}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
				vars, ok := result["variables"].(map[string]interface{})
				require.True(t, ok)
				k, ok := vars["k"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, true, k["allowOverride"])
			},
		},
		{
			name:        "Includes allowOverride when false",
			existingDef: map[string]interface{}{},
			inputVars:   map[string]adoVariable{"k": {Value: "v", AllowOverride: &allowFalse}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
				vars, ok := result["variables"].(map[string]interface{})
				require.True(t, ok)
				k, ok := vars["k"].(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, false, k["allowOverride"])
			},
		},
		{
			name:        "Returns valid JSON",
			existingDef: map[string]interface{}{"name": "test"},
			inputVars:   map[string]adoVariable{"v1": {Value: "x"}},
			assertions: func(t *testing.T, body []byte, err error) {
				require.NoError(t, err)
				require.NotNil(t, body)
				var result map[string]interface{}
				err = json.Unmarshal(body, &result)
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, err := buildVariablesUpdatePayload(tt.existingDef, tt.inputVars)
			tt.assertions(t, body, err)
		})
	}
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

// TestBuildURL_ParseErrors verifies that URL-building functions return the
// expected wrapped error when url.Parse fails due to an invalid BaseURL
// containing a control character (e.g., newline).
func TestBuildURL_ParseErrors(t *testing.T) {
	// BaseURL with newline causes url.Parse to fail with "invalid control character"
	m := &AdoManager{BaseURL: "http://x\ny", httpClient: &http.Client{}}

	t.Run("buildGetFileContentURL - parse error", func(t *testing.T) {
		t.Parallel()
		_, err := m.buildGetFileContentURL("org", "proj", "repo", "/path", "main")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base URL")
	})

	t.Run("buildListRepositoryItemsURL - parse error", func(t *testing.T) {
		t.Parallel()
		_, err := m.buildListRepositoryItemsURL("org", "proj", "repo", "/", "main", "none")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base URL")
	})

	t.Run("buildListPullRequestsURL - parse error", func(t *testing.T) {
		t.Parallel()
		_, err := m.buildListPullRequestsURL("org", "proj", "repo", "active", 50)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base URL")
	})

	t.Run("fetchPrStatuses - URL parse error", func(t *testing.T) {
		t.Parallel()
		// fetchPrStatuses calls url.Parse before m.ExecuteRequest.
		// With an invalid BaseURL, url.Parse fails before any HTTP call.
		_, err := m.fetchPrStatuses(context.Background(), "org", "proj", "repo", 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse statuses base URL")
	})

	t.Run("fetchPolicyEvaluations - URL parse error", func(t *testing.T) {
		t.Parallel()
		// fetchPolicyEvaluations calls url.Parse before m.ExecuteRequest.
		// With an invalid BaseURL, url.Parse fails before any HTTP call.
		_, err := m.fetchPolicyEvaluations(context.Background(), "org", "proj", "artifact-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse policy base URL")
	})
}
