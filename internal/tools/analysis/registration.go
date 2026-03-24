// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Register adds all consolidated analysis tools to the registry.
func Register(r tools.Registry, sm domain_security.Manager, bus events.EventBus, executor tools.CommandExecutor, fs persistence.FileSystem) error {
	idx, _ := newIndexer(".")
	cache := newASTCache()
	m := newAnalysisManager(idx, cache, sm, bus, executor, fs)

	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
		opts    *tools.ToolOptions
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "verify_architecture",
				Description: "Map component dependencies and identify 'God Objects' or circular references. Verifies adherence to Hexagonal/Clean Architecture layers.",
			},
			handler: m.Arch.VerifyArchitecture,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_code_health",
				Description: "Returns a high-level summary of project health, including test status, coverage, linting issues, and complexity alerts. Use this to verify system integrity after major refactors.",
			},
			handler: m.Health.GetCodeHealth,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_detailed_coverage",
				Description: "Analyzes Go test coverage to identify specific untested code blocks, prioritizing error handling and business logic gaps.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {Type: "STRING", Description: "The package path to analyze (e.g., './internal/service/...')"},
					},
					Required: []string{"path"},
				},
			},
			handler: m.Health.getDetailedCoverage,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "find_usages",
				Description: "Uses static analysis (AST) to find all precise references to a specific Go symbol. Use this for accurate refactoring or impact analysis.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"query": {Type: "STRING", Description: "The symbol name to find."},
						"path":  {Type: "STRING", Description: "The directory to search (defaults to '.')"},
					},
					Required: []string{"query"},
				},
			},
			handler: m.FindUsages,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "find_definitions",
				Description: "Finds the exact declaration(s) of a symbol using AST.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"query": {Type: "STRING", Description: "The symbol name to find."},
						"path":  {Type: "STRING", Description: "The directory to search (defaults to '.')"},
					},
					Required: []string{"query"},
				},
			},
			handler: m.FindDefinitions,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_symbols",
				Description: "Lists all top-level symbols (functions, types, constants, variables) in a package or directory.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path":          {Type: "STRING", Description: "The directory to scan (defaults to '.')"},
						"exported_only": {Type: "BOOLEAN", Description: "If true, only lists exported symbols."},
					},
				},
			},
			handler: m.ListSymbols,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_implementations",
				Description: "Map the relationship between interfaces and structs in the codebase.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"interface_name": {Type: "STRING", Description: "The name of the interface to find implementors for."},
					},
					Required: []string{"interface_name"},
				},
			},
			handler: m.ListImplementations,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_type_info",
				Description: "Provides a detailed structural breakdown of a Go type, including fields, and all associated methods. Use this to understand internal state and behavior.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"typename": {Type: "STRING", Description: "The name of the type to inspect."},
						"path":     {Type: "STRING", Description: "The directory to search for the type (defaults to '.')"},
					},
					Required: []string{"typename"},
				},
			},
			handler: m.GetTypeInfo,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_project_summary",
				Description: "Returns a high-level summary of the project architecture, including packages, file counts, and Go module info.",
			},
			handler: m.Info.GetProjectSummary,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_file_skeleton",
				Description: "Extracts the public API surface of a source file, including all exported types and function signatures, while omitting implementations. HINT: This tool ONLY returns signatures. If you need to read the actual code/body of a function, you MUST use the 'read_files' tool.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"filepath": {Type: "STRING", Description: "The path to the source code file."},
					},
					Required: []string{"filepath"},
				},
			},
			handler: m.Info.GetFileSkeleton,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "search_usages_globally",
				Description: "Performs a high-speed text search across all non-ignored project files. Use this for non-code files (YAML, MD) or finding hardcoded strings.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"query":    {Type: "STRING", Description: "The text or regex to search for."},
						"path":     {Type: "STRING", Description: "The directory to search (defaults to '.')"},
						"is_regex": {Type: "BOOLEAN", Description: "If true, treats query as a regex. Default is false (literal string search)."},
					},
					Required: []string{"query"},
				},
			},
			handler: m.Search.SearchUsagesGlobally,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_semantic_diff",
				Description: "Analyzes Go code changes between the current state and a Git target using AST comparison. Summarizes logical changes rather than raw line diffs.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"target": {Type: "STRING", Description: "The git target (commit hash, branch name, or 'HEAD~1') to compare against."},
					},
					Required: []string{"target"},
				},
			},
			handler: m.SemanticDiff,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:            "rename_symbol",
				Description:     "Safely renames a Go symbol (function, type, variable) across the project using AST.",
				RequiresConsent: true,
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"old_name": {Type: "STRING", Description: "The current name of the symbol."},
						"new_name": {Type: "STRING", Description: "The new name for the symbol."},
						"path":     {Type: "STRING", Description: "The directory to search (defaults to '.')"},
						"reason":   {Type: "STRING", Description: "Reason for this refactoring."},
					},
					Required: []string{"old_name", "new_name", "reason"},
				},
			},
			handler: m.Refactor.RenameSymbol,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_todos",
				Description: "Scans the project for TODO, FIXME, or BUG comments.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {Type: "STRING", Description: "The directory to scan (defaults to '.')"},
					},
				},
			},
			handler: m.Search.ListTodos,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "go_doc",
				Description: "Retrieves the official Go documentation and comments for a symbol or package. Best for understanding the intended usage of a library.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"symbol": {Type: "STRING", Description: "The package or symbol to get documentation for (e.g., 'fmt.Println', './internal/tools')."},
					},
					Required: []string{"symbol"},
				},
			},
			handler: m.Info.GoDoc,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_complexity_metrics",
				Description: "Calculates the cyclomatic complexity of Go functions in a file or directory.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {Type: "STRING", Description: "The file or directory to analyze."},
					},
					Required: []string{"path"},
				},
			},
			handler: m.AnalyzeComplexity,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "get_package_graph",
				Description: "Returns a mapping of internal package dependencies.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"format": {Type: "STRING", Description: "Output format: 'text' (default) or 'mermaid'.", Enum: []string{"text", "mermaid"}},
					},
				},
			},
			handler: m.GetPackageGraph,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "analyze_sequence_flow",
				Description: "Trace a specific function call across package boundaries to generate a Mermaid Sequence Diagram.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"start_symbol": {Type: "STRING", Description: "The fully qualified name of a function (e.g., 'internal/api/handler.CreateUser')."},
						"max_depth":    {Type: "INTEGER", Description: "Maximum recursion depth (default 5)."},
					},
					Required: []string{"start_symbol"},
				},
			},
			handler: m.AnalyzeSequenceFlow,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "dead_code_graph",
				Description: "Identify exported symbols with zero inbound references within the module. This is a heavy operation that requires a go.mod file and scans the entire module to find technical debt.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {Type: "STRING", Description: "The directory to search (defaults to '.')"},
						"excluded_packages": {
							Type:        "ARRAY",
							Items:       &tools.Schema{Type: "STRING"},
							Description: "Patterns of packages to ignore.",
						},
					},
				},
			},
			handler: m.FindOrphanedSymbols,
			opts:    &tools.ToolOptions{LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:            "move_definition",
				Description:     "Moves a Go symbol (struct, interface, function) and its associated methods from one file to another.",
				RequiresConsent: true,
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"symbol":   {Type: "STRING", Description: "The name of the symbol to move."},
						"src_file": {Type: "STRING", Description: "The source file path."},
						"dst_file": {Type: "STRING", Description: "The destination file path."},
						"reason":   {Type: "STRING", Description: "Reason for this move."},
					},
					Required: []string{"symbol", "src_file", "dst_file", "reason"},
				},
			},
			handler: m.Refactor.MoveDefinition,
			opts:    &tools.ToolOptions{Serial: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "generate_mermaid_diagram",
				Description: "transform a package dependency graph into Mermaid.js 'graph TD' syntax.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"graph": {Type: "OBJECT", Description: "A map where keys are package names and values are lists of dependencies."},
					},
					Required: []string{"graph"},
				},
			},
			handler: generateMermaidDiagram,
		},
	}

	for _, spec := range specs {
		if spec.opts != nil {
			if err := r.RegisterWithOptions(spec.decl, spec.handler, *spec.opts); err != nil {
				return err
			}
		} else {
			if err := r.Register(spec.decl, spec.handler); err != nil {
				return err
			}
		}
	}

	return nil
}
