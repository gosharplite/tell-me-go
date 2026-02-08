// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package network

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// Register adds network-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	net := NewNetworkTool(sm, nil)
	RegisterTeams(r, sm, nil)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches and cleans content from a URL, stripping HTML tags and scripts to provide readable documentation. Useful for researching library APIs.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"url": {
					Type:        "STRING",
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, net.ReadExternalDocs, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"method": {
					Type:        "STRING",
					Description: "HTTP method (GET, POST, PUT, DELETE, etc.).",
				},
				"url": {
					Type:        "STRING",
					Description: "The target URL.",
				},
				"headers": {
					Type:        "OBJECT",
					Description: "HTTP headers as a map of strings.",
					Properties: map[string]*tools.Schema{
						"Content-Type": {Type: "STRING"},
					},
				},
				"body": {
					Type:        "STRING",
					Description: "Request body content.",
				},
			},
			Required: []string{"method", "url"},
		},
	}, net.HttpRequest, registry.ToolOptions{LongRunning: true})
}
