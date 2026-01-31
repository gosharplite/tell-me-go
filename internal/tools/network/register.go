// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package network

import (
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// Register adds network-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	net := NewNetworkTool(sm)
	RegisterTeams(r, sm)

	r.Register(&types.ToolDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches and cleans content from a URL, stripping HTML tags and scripts to provide readable documentation. Useful for researching library APIs.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"url": {
					Type:        "STRING",
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, net.ReadExternalDocs)

	r.Register(&types.ToolDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
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
					Properties: map[string]*types.Schema{
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
	}, net.HttpRequest)
}
