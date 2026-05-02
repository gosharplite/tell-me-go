// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// registerBuilds registers the Build-category ADO tools (timeline, task log, build changes).
// Handlers: m.GetBuildTimeline, m.GetTaskLog, m.GetBuildChanges (defined in pipeline_runs.go / pipeline_crud.go).
func registerBuilds(r tools.Registry, m *AdoManager, f PipelineFormatter) error {
	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_build_timeline",
				Description: "Retrieves the build timeline, providing the state, result, and log metadata for every task in the build.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"build_id":     {Type: "INTEGER", Description: "The numeric ID of the build (not the pipeline ID)."},
					},
					Required: []string{"organization", "project", "build_id"},
				},
			},
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				records, err := m.GetBuildTimeline(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return marshalIndentResult(records)
			},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_task_log",
				Description: "Retrieves the raw console output/logs for a specific build task.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"build_id":      {Type: "INTEGER", Description: "The numeric ID of the build."},
						"log_id":        {Type: "INTEGER", Description: "The numeric ID of the specific log record (retrieved from the build timeline)."},
						"tail_lines":    {Type: "INTEGER", Description: "Return the last N lines. Default: 200"},
						"head_lines":    {Type: "INTEGER", Description: "Return the first N lines (optional)."},
						"filter_query":  {Type: "STRING", Description: "Regex to filter log lines (e.g. 'error|failed'). WARNING: This parameter OVERRIDES tail/head/pagination. It scans the ENTIRE file. Do NOT use greedy wildcards like '.*' as it will return massive payloads and crash the context."},
						"context_lines": {Type: "INTEGER", Description: "Lines of context around filter matches. Default: 5"},
						"start_line":    {Type: "INTEGER", Description: "Start line for pagination."},
						"max_lines":     {Type: "INTEGER", Description: "Maximum lines for pagination."},
					},
					Required: []string{"organization", "project", "build_id", "log_id"},
				},
			},
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				content, err := m.GetTaskLog(ctx, args, hb)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: appendTruncationNote(content)}, nil
			},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_build_changes",
				Description: "Retrieves the list of commits/changes included in a specific build.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"build_id":     {Type: "INTEGER", Description: "The numeric ID of the build."},
						"top":          {Type: "INTEGER", Description: "Maximum number of changes to return (default 50)."},
					},
					Required: []string{"organization", "project", "build_id"},
				},
			},
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				changes, err := m.GetBuildChanges(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return marshalIndentResult(changes)
			},
		},
	}

	for _, spec := range specs {
		if err := r.RegisterToToolkit("ado", spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
