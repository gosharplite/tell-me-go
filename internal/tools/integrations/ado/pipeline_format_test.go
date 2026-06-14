// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatPipelineRunsList(t *testing.T) {
	tests := []struct {
		name       string
		pipelineID int
		runs       []adoPipelineRun
		expect     string // exact match for sentinel
		contains   []string
	}{
		{
			name:       "Empty list returns sentinel",
			pipelineID: 1,
			runs:       nil,
			expect:     "No pipeline runs found.",
		},
		{
			name:       "Single run",
			pipelineID: 42,
			runs: []adoPipelineRun{
				{
					Id:      101,
					Name:    "20260101.1",
					State:   "completed",
					Result:  "succeeded",
					Created: "2026-01-01T00:00:00Z",
					Repository: struct {
						Id   string `json:"id"`
						Name string `json:"name"`
					}{Id: "repo-guid", Name: "my-repo"},
				},
			},
			contains: []string{
				"Recent runs for pipeline 42:",
				"Run ID: 101",
				"Status: completed",
				"Result: succeeded",
				"Repo: my-repo",
			},
		},
		{
			name:       "Multiple runs",
			pipelineID: 7,
			runs: []adoPipelineRun{
				{
					Id:      1,
					Name:    "run-a",
					State:   "inProgress",
					Result:  "unknown",
					Created: "d1",
					Repository: struct {
						Id   string `json:"id"`
						Name string `json:"name"`
					}{Name: "r1"},
				},
				{
					Id:      2,
					Name:    "run-b",
					State:   "completed",
					Result:  "failed",
					Created: "d2",
					Repository: struct {
						Id   string `json:"id"`
						Name string `json:"name"`
					}{Name: "r2"},
				},
			},
			contains: []string{
				"Run ID: 1",
				"Run ID: 2",
				"r1",
				"r2",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFormatter()
			got := f.FormatPipelineRunsList(tt.pipelineID, tt.runs)

			if tt.expect != "" {
				assert.Equal(t, tt.expect, got)
			}
			for _, s := range tt.contains {
				assert.Contains(t, got, s)
			}
		})
	}
}

func TestFormatPipelineRunDetail(t *testing.T) {
	tests := []struct {
		name     string
		run      *adoPipelineRunDetail
		contains []string
	}{
		{
			name: "Full detail",
			run: &adoPipelineRunDetail{
				Id:      42,
				Name:    "build-42",
				State:   "completed",
				Result:  "succeeded",
				Created: "2026-01-01",
				Url:     "https://dev.azure.com/org/proj/_build/results?buildId=42",
			},
			contains: []string{
				"Pipeline Run #42 Details:",
				"Name: build-42",
				"Status: completed",
				"Result: succeeded",
				"Created: 2026-01-01",
				"URL: https://dev.azure.com/org/proj/_build/results?buildId=42",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFormatter()
			got := f.FormatPipelineRunDetail(tt.run)

			for _, s := range tt.contains {
				assert.Contains(t, got, s)
			}
		})
	}
}

func TestFormatPipelineLogList(t *testing.T) {
	tests := []struct {
		name     string
		runID    int
		logs     []adoLogEntry
		expect   string // exact match for sentinel
		contains []string
	}{
		{
			name:   "Empty list returns sentinel",
			runID:  1,
			logs:   nil,
			expect: "No logs found for this run.",
		},
		{
			name:  "Single log",
			runID: 101,
			logs: []adoLogEntry{
				{Id: 5, Line: 200},
			},
			contains: []string{
				"Logs for Pipeline Run #101:",
				"Log ID: 5 (200 lines)",
				"provide a log_id",
			},
		},
		{
			name:  "Multiple logs",
			runID: 202,
			logs: []adoLogEntry{
				{Id: 1, Line: 10},
				{Id: 2, Line: 500},
			},
			contains: []string{
				"Log ID: 1",
				"Log ID: 2",
				"provide a log_id",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPipelineFormatter()
			got := f.FormatPipelineLogList(tt.runID, tt.logs)

			if tt.expect != "" {
				assert.Equal(t, tt.expect, got)
			}
			for _, s := range tt.contains {
				assert.Contains(t, got, s)
			}
		})
	}
}

func TestFormatRepositoryItems(t *testing.T) {
	tests := []struct {
		name        string
		scopePath   string
		version     string
		response    adoRepositoryItemsResponse
		expect      string   // exact match
		contains    []string // substrings that must appear
		notContains []string // substrings that must NOT appear
	}{
		{
			name:      "Empty Value slice returns sentinel",
			scopePath: "/",
			version:   "main",
			response: adoRepositoryItemsResponse{
				Value: nil,
				Count: 0,
			},
			expect: "No items found.",
		},
		{
			name:      "Single item matching scopePath, len==1 shows the item",
			scopePath: "/",
			version:   "main",
			response: adoRepositoryItemsResponse{
				Value: []struct {
					Path     string `json:"path"`
					IsFolder bool   `json:"isFolder"`
				}{
					{Path: "/", IsFolder: true},
				},
				Count: 1,
			},
			contains: []string{"[DIR]  /"},
		},
		{
			name:      "Two items, first matches scopePath, len>1 skips root dir",
			scopePath: "/",
			version:   "main",
			response: adoRepositoryItemsResponse{
				Value: []struct {
					Path     string `json:"path"`
					IsFolder bool   `json:"isFolder"`
				}{
					{Path: "/", IsFolder: true},
					{Path: "/src/main.go", IsFolder: false},
				},
				Count: 2,
			},
			contains:    []string{"[FILE] /src/main.go"},
			notContains: []string{"[DIR]  /"},
		},
		{
			name:      "Non-matching scopePath, no filtering",
			scopePath: "/src",
			version:   "main",
			response: adoRepositoryItemsResponse{
				Value: []struct {
					Path     string `json:"path"`
					IsFolder bool   `json:"isFolder"`
				}{
					{Path: "/src/main.go", IsFolder: false},
				},
				Count: 1,
			},
			contains: []string{"[FILE] /src/main.go"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatRepositoryItems(tt.scopePath, tt.version, tt.response)

			if tt.expect != "" {
				assert.Equal(t, tt.expect, result.Text)
			}
			for _, s := range tt.contains {
				assert.Contains(t, result.Text, s)
			}
			for _, s := range tt.notContains {
				assert.NotContains(t, result.Text, s)
			}
		})
	}
}
