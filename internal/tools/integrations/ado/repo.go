// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// registerRepository registers the Repository-category ADO tools (file content, list items).
// Handlers: m.adoGetFileContent, m.AdoListRepositoryItems (defined in azure_devops.go).
func registerRepository(r tools.Registry, m *AdoManager, _ PipelineFormatter) error {
	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_file_content",
				Description: "Retrieves the content of a specific file from an Azure DevOps repository at a given path and version (branch/commit).",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"repository":   {Type: "STRING", Description: "The repository name or ID."},
						"path":         {Type: "STRING", Description: "The full path to the file in the repository (e.g., '/src/main.go')."},
						"version":      {Type: "STRING", Description: "The branch name, commit hash, or tag. Default is main."},
					},
					Required: []string{"organization", "project", "repository", "path"},
				},
			},
			handler: m.adoGetFileContent,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_repository_items",
				Description: "Lists items (files and folders) in a specific directory of an Azure DevOps repository.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"scope_path":      {Type: "STRING", Description: "The directory path to list. Default is / (root)."},
						"version":         {Type: "STRING", Description: "The branch name, commit hash, or tag. Default is main."},
						"recursion_level": {Type: "STRING", Description: "Recursion level: none (default), oneLevel, or full."},
					},
					Required: []string{"organization", "project", "repository"},
				},
			},
			handler: m.AdoListRepositoryItems,
		},
	}

	for _, spec := range specs {
		if err := r.RegisterToToolkit("ado", spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
