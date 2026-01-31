// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// Config holds the configuration for framework tools.
type Config struct {
	LogFile          string
	Model            string
	Mode             string
	OutputDir        string
	PricingOverrides map[string]types.ModelPricing
}

// Register adds framework-related tools (policy, metrics, state) to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager, cfg Config) {
	// 1. Policy Tools
	policy := NewPolicyTool(sm)
	registerPolicyTools(r, policy)

	// 2. Metrics Tools
	RegisterMetrics(r, sm, cfg.LogFile, cfg.Model, cfg.Mode, cfg.PricingOverrides)

	// 3. State Tools
	RegisterState(r, sm, cfg.OutputDir)
}

func registerPolicyTools(r *registry.Registry, policy *PolicyTool) {
	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "register_safepath",
		Description: "Adds a path to the persistent 'safe' list, allowing future AI sessions to read/write in that location without repeating security authorizations.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The absolute or relative path to authorize.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason why this path needs to be authorized.",
				},
			},
			Required: []string{"path", "reason"},
		},
	}, policy.RegisterSafePath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, policy.ListSafePaths)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "remove_safepath",
		Description: "Removes a directory or file from the authorized boundaries.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The path to remove from authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, policy.RemoveSafePath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "register_readpath",
		Description: "Adds a directory or file to the allowed boundaries for READ-ONLY access. This is a persistent configuration.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The absolute or relative path to authorize for reading.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason why this path needs to be authorized.",
				},
			},
			Required: []string{"path", "reason"},
		},
	}, policy.RegisterReadPath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, policy.ListReadPaths)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "remove_readpath",
		Description: "Removes a directory or file from the read-only authorized boundaries.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The path to remove from read-only authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, policy.RemoveReadPath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, policy.BypassConfirmation, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, policy.RevokeBypass, registry.ToolOptions{Serial: true, LongRunning: true})
}
