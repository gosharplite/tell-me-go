// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Register adds all development workflow and release tools to the registry.
func Register(r tools.Registry, sm domain_security.Manager, exec tools.CommandExecutor, validator domain_security.CommandValidator, fs persistence.FileSystem) error {
	dev := newDevManager(sm, validator)

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, dev.runTests, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "go_tidy",
		Description: "Runs 'go mod tidy' and 'go fmt ./...'.",
	}, dev.goTidy, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, dev.getCoverage, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_linter",
		Description: "Runs the first available linter (golangci-lint or staticcheck). Returns a list of findings or success message.",
	}, dev.runLinter, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, dev.runBenchmark, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "check_vulnerabilities",
		Description: "Runs 'govulncheck'.",
	}, dev.checkVulnerabilities, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	// Release Management
	rel := &releaseManager{
		sm:       sm,
		fs:       fs,
		executor: exec,
	}
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "verify_release_readiness",
		Description: "Performs an automated check of all SOP release requirements (clean build, secret scanning, go.mod check, and test execution).",
	}, rel.verifyReleaseReadiness, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}
	return nil
}
