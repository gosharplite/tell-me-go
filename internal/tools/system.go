// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"github.com/gosharplite/tell-me-go/internal/types"
)

// RegisterSystemTools adds system-related tools to the registry.
func RegisterSystemTools(r *Registry, sm *SecurityManager) {
	shell := NewShellTool(sm)
	net := &NetworkTool{sm: sm}
	policy := &PolicyTool{sm: sm}
	interaction := &InteractionTool{sm: sm}

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "execute_command",
		Description: "Executes a single shell command without shell interpretation (direct binary call). Security: Only whitelisted commands are auto-approved; others require user confirmation.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"command": {
					Type:        "STRING",
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this command needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"command"},
		},
	}, shell.ExecuteCommand, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "pipe_commands",
		Description: "Executes a sequence of commands by piping the output of each to the next. Security: All commands in the pipe must be whitelisted for auto-approval.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"commands": {
					Type: "ARRAY",
					Items: &types.Schema{
						Type: "STRING",
					},
					Description: "The sequence of commands to pipe (e.g., ['ls -la', 'grep .go']).",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this pipeline needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect the final output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"commands"},
		},
	}, shell.PipeCommands, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "ask_user",
		Description: "Asks the user a specific question to clarify requirements or request confirmation.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"question": {
					Type:        "STRING",
					Description: "The question to ask the user.",
				},
			},
			Required: []string{"question"},
		},
	}, interaction.AskUser, ToolOptions{Serial: true, LongRunning: true})

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
	}, policy.RegisterSafePath, ToolOptions{Serial: true, LongRunning: true})

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
	}, policy.RemoveSafePath, ToolOptions{Serial: true, LongRunning: true})

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
	}, policy.RegisterReadPath, ToolOptions{Serial: true, LongRunning: true})

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
	}, policy.RemoveReadPath, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, policy.BypassConfirmation, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, policy.RevokeBypass, ToolOptions{Serial: true, LongRunning: true})
}
