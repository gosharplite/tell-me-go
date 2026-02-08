// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// Register adds all consolidated analysis and development tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	idx, _ := NewIndexer(".")
	cache := NewASTCache()
	m := NewAnalysisManager(idx, cache, sm)

	// Code Analysis Tools
	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "verify_architecture",
		Description: "Map component dependencies and identify 'God Objects' or circular references. Verifies adherence to Hexagonal/Clean Architecture layers.",
	}, m.Arch.VerifyArchitecture, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_code_health",
		Description: "Returns a high-level summary of project health, including test status, coverage, linting issues, and complexity alerts. Use this to verify system integrity after major refactors.",
	}, m.Health.GetCodeHealth, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_detailed_coverage",
		Description: "Analyzes Go test coverage to identify specific untested code blocks, prioritizing error handling and business logic gaps.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The package path to analyze (e.g., './internal/service/...')",
				},
			},
			Required: []string{"path"},
		},
	}, m.Health.GetDetailedCoverage, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "find_usages",
		Description: "Uses static analysis (AST) to find all precise references to a specific Go symbol. Use this for accurate refactoring or impact analysis.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.FindUsages, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "find_definitions",
		Description: "Finds the exact declaration(s) of a symbol using AST.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.FindDefinitions, registry.ToolOptions{LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "list_symbols",
		Description: "Lists all top-level symbols (functions, types, constants, variables) in a package or directory.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.ListSymbols)

	r.Register(&tools.ToolDeclaration{
		Name:        "list_implementations",
		Description: "Map the relationship between interfaces and structs in the codebase.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"interface_name": {
					Type:        "STRING",
					Description: "The name of the interface to find implementors for.",
				},
			},
			Required: []string{"interface_name"},
		},
	}, m.ListImplementations)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_type_info",
		Description: "Provides a detailed structural breakdown of a Go type, including fields, and all associated methods. Use this to understand internal state and behavior.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.GetTypeInfo)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_project_summary",
		Description: "Returns a high-level summary of the project architecture, including packages, file counts, and Go module info.",
	}, m.Info.GetProjectSummary)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_file_skeleton",
		Description: "Extracts the public API surface of a source file, including all exported types and function signatures, while omitting implementations.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the source code file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.Info.GetFileSkeleton)

	r.Register(&tools.ToolDeclaration{
		Name:        "search_usages_globally",
		Description: "Performs a high-speed text search across all non-ignored project files. Use this for non-code files (YAML, MD) or finding hardcoded strings.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"query": {
					Type:        "STRING",
					Description: "The string or regex to search for.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"query"},
		},
	}, m.Search.SearchUsagesGlobally)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_semantic_diff",
		Description: "Analyzes Go code changes between the current state and a Git target using AST comparison. Summarizes logical changes rather than raw line diffs.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"target": {
					Type:        "STRING",
					Description: "The git target (commit hash, branch name, or 'HEAD~1') to compare against.",
				},
			},
			Required: []string{"target"},
		},
	}, m.SemanticDiff)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "rename_symbol",
		Description: "Safely renames a Go symbol (function, type, variable) across the project using AST.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.Refactor.RenameSymbol, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "list_todos",
		Description: "Scans the project for TODO, FIXME, or BUG comments.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to scan (defaults to '.')",
				},
			},
		},
	}, m.Search.ListTodos)

	r.Register(&tools.ToolDeclaration{
		Name:        "go_doc",
		Description: "Retrieves the official Go documentation and comments for a symbol or package. Best for understanding the intended usage of a library.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"symbol": {
					Type:        "STRING",
					Description: "The package or symbol to get documentation for (e.g., 'fmt.Println', './internal/tools').",
				},
			},
			Required: []string{"symbol"},
		},
	}, m.Info.GoDoc)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_complexity_metrics",
		Description: "Calculates the cyclomatic complexity of Go functions in a file or directory.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The file or directory to analyze.",
				},
			},
			Required: []string{"path"},
		},
	}, m.AnalyzeComplexity)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_package_graph",
		Description: "Returns a mapping of internal package dependencies.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"format": {
					Type:        "STRING",
					Description: "Output format: 'text' (default) or 'mermaid'.",
					Enum:        []string{"text", "mermaid"},
				},
			},
		},
	}, m.GetPackageGraph)

	r.Register(&tools.ToolDeclaration{
		Name:        "analyze_sequence_flow",
		Description: "Trace a specific function call across package boundaries to generate a Mermaid Sequence Diagram.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"start_symbol": {
					Type:        "STRING",
					Description: "The fully qualified name of a function (e.g., 'internal/api/handler.CreateUser').",
				},
				"max_depth": {
					Type:        "INTEGER",
					Description: "Maximum recursion depth (default 5).",
				},
			},
			Required: []string{"start_symbol"},
		},
	}, m.AnalyzeSequenceFlow)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "dead_code_graph",
		Description: "Identify exported symbols with zero inbound references within the module. This is a heavy operation that requires a go.mod file and scans the entire module to find technical debt.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
				"excluded_packages": {
					Type: "ARRAY",
					Items: &tools.Schema{
						Type: "STRING",
					},
					Description: "Patterns of packages to ignore.",
				},
			},
		},
	}, m.FindOrphanedSymbols, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "move_definition",
		Description: "Moves a Go symbol (struct, interface, function) and its associated methods from one file to another.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.Refactor.MoveDefinition, registry.ToolOptions{Serial: true})

	// Dev Tools
	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_tests",
		Description: "Executes project tests using authorized tools (go, pytest, npm, cargo, make). Returns 'PASS' or truncated failure logs. Shell metacharacters are forbidden for security.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"command": {
					Type:        "STRING",
					Description: "The test command to execute (e.g., 'go test ./...', 'npm test').",
				},
			},
			Required: []string{"command"},
		},
	}, m.Dev.runTests, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "go_tidy",
		Description: "Runs 'go mod tidy' and 'go fmt ./...'.",
	}, m.Dev.goTidy, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_coverage",
		Description: "Runs Go tests with coverage and returns the summary.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The package path to test (default './...')",
				},
			},
		},
	}, m.Dev.getCoverage, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_linter",
		Description: "Runs the first available linter (golangci-lint or staticcheck). Returns a list of findings or success message.",
	}, m.Dev.runLinter, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_benchmark",
		Description: "Runs Go benchmarks and returns performance metrics (ns/op, B/op).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The package path to benchmark (default './...')",
				},
				"bench": {
					Type:        "STRING",
					Description: "Regex for benchmarks to run (default '.')",
				},
			},
		},
	}, m.Dev.runBenchmark, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "check_vulnerabilities",
		Description: "Runs 'govulncheck'.",
	}, m.Dev.checkVulnerabilities, registry.ToolOptions{LongRunning: true})

	// Release Management
	rel := &releaseManager{
		sm:       sm,
		fs:       storage.DefaultFileSystem,
		executor: workspace.NewProcessExecutor(),
	}
	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "verify_release_readiness",
		Description: "Performs an automated check of all SOP release requirements (clean build, secret scanning, go.mod check, and test execution).",
	}, rel.verifyReleaseReadiness, registry.ToolOptions{Serial: true, LongRunning: true})
}
