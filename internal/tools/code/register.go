// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// Register adds AST-based tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	nav := &NavigationManager{SP: sm}
	ref := &RefactorManager{SP: sm}
	ana := &AnalysisManager{SP: sm}
	inf := &InfoManager{SP: sm}
	sea := &SearchManager{SP: sm}

	r.Register(&types.ToolDeclaration{
		Name:        "find_usages",
		Description: "Uses static analysis (AST) to find all precise references to a specific Go symbol. Use this for accurate refactoring or impact analysis.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"query": {
					Type:        "STRING",
					Description: "The symbol name to find.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"query"},
		},
	}, nav.FindUsages)

	r.Register(&types.ToolDeclaration{
		Name:        "find_definitions",
		Description: "Finds the exact declaration(s) of a symbol using AST.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"query": {
					Type:        "STRING",
					Description: "The symbol name to find.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"query"},
		},
	}, nav.FindDefinitions)

	r.Register(&types.ToolDeclaration{
		Name:        "list_symbols",
		Description: "Lists all top-level symbols (functions, types, constants, variables) in a package or directory.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to scan (defaults to '.')",
				},
				"exported_only": {
					Type:        "BOOLEAN",
					Description: "If true, only lists exported symbols.",
				},
			},
		},
	}, nav.ListSymbols)

	r.Register(&types.ToolDeclaration{
		Name:        "list_implementations",
		Description: "Map the relationship between interfaces and structs in the codebase.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
		},
	}, nav.ListImplementations)

	r.Register(&types.ToolDeclaration{
		Name:        "get_type_info",
		Description: "Provides a detailed structural breakdown of a Go type, including fields, and all associated methods. Use this to understand internal state and behavior.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"typename": {
					Type:        "STRING",
					Description: "The name of the type to inspect.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search for the type (defaults to '.')",
				},
			},
			Required: []string{"typename"},
		},
	}, nav.GetTypeInfo)

	r.Register(&types.ToolDeclaration{
		Name:        "get_project_summary",
		Description: "Returns a high-level summary of the project architecture, including packages, file counts, and Go module info.",
	}, inf.GetProjectSummary)

	r.Register(&types.ToolDeclaration{
		Name:        "search_usages_globally",
		Description: "Performs a high-speed text search across all non-ignored project files. Use this for non-code files (YAML, MD) or finding hardcoded strings.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"query": {
					Type:        "STRING",
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, sea.SearchUsagesGlobally)

	r.Register(&types.ToolDeclaration{
		Name:        "semantic_diff",
		Description: "Analyzes Go code changes between the current state and a Git target using AST comparison. Summarizes logical changes rather than raw line diffs.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"target": {
					Type:        "STRING",
					Description: "The git target (commit hash, branch name, or 'HEAD~1') to compare against.",
				},
			},
			Required: []string{"target"},
		},
	}, ana.SemanticDiff)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "rename_symbol",
		Description: "Safely renames a Go symbol (function, type, variable) across the project using AST.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"old_name": {
					Type:        "STRING",
					Description: "The current name of the symbol.",
				},
				"new_name": {
					Type:        "STRING",
					Description: "The new name for the symbol.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for this refactoring.",
				},
			},
			Required: []string{"old_name", "new_name", "reason"},
		},
	}, ref.RenameSymbol, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_todos",
		Description: "Scans the project for TODO, FIXME, or BUG comments.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to scan (defaults to '.')",
				},
			},
		},
	}, sea.ListTodos)

	r.Register(&types.ToolDeclaration{
		Name:        "go_doc",
		Description: "Retrieves the official Go documentation and comments for a symbol or package. Best for understanding the intended usage of a library.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"symbol": {
					Type:        "STRING",
					Description: "The package or symbol to get documentation for (e.g., 'fmt.Println', './internal/tools').",
				},
			},
			Required: []string{"symbol"},
		},
	}, inf.GoDoc)

	r.Register(&types.ToolDeclaration{
		Name:        "analyze_complexity",
		Description: "Calculates the cyclomatic complexity of Go functions in a file or directory.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The file or directory to analyze.",
				},
			},
			Required: []string{"path"},
		},
	}, ana.AnalyzeComplexity)

	r.Register(&types.ToolDeclaration{
		Name:        "get_package_graph",
		Description: "Returns a mapping of internal package dependencies.",
	}, ana.GetPackageGraph)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "move_definition",
		Description: "Moves a Go symbol (struct, interface, function) and its associated methods from one file to another.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"symbol": {
					Type:        "STRING",
					Description: "The name of the symbol to move.",
				},
				"src_file": {
					Type:        "STRING",
					Description: "The source file path.",
				},
				"dst_file": {
					Type:        "STRING",
					Description: "The destination file path.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for this move.",
				},
			},
			Required: []string{"symbol", "src_file", "dst_file", "reason"},
		},
	}, ref.MoveDefinition, registry.ToolOptions{Serial: true})
}
