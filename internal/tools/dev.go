// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"google.golang.org/genai"
)

// RegisterDevTools adds developer-related tools to the registry.
func RegisterDevTools(r *Registry) {
	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "run_tests",
		Description: "Executes project tests (Go, Python, NPM, etc.) and returns the truncated output.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"command": {
					Type:        genai.TypeString,
					Description: "The test command to execute (e.g., 'go test ./...', 'npm test').",
				},
			},
			Required: []string{"command"},
		},
	}, runTests, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "go_tidy",
		Description: "Runs 'go mod tidy' and 'go fmt ./...'.",
	}, goTidy, ToolOptions{Serial: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_coverage",
		Description: "Runs Go tests with coverage and returns the summary.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The package path to test (default './...')",
				},
			},
		},
	}, getCoverage)

	r.Register(&genai.FunctionDeclaration{
		Name:        "run_linter",
		Description: "Runs 'staticcheck' or 'golangci-lint' on the project.",
	}, runLinter)

	r.Register(&genai.FunctionDeclaration{
		Name:        "run_benchmark",
		Description: "Runs Go benchmarks and returns performance metrics (ns/op, B/op).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The package path to benchmark (default './...')",
				},
				"bench": {
					Type:        genai.TypeString,
					Description: "Regex for benchmarks to run (default '.')",
				},
			},
		},
	}, runBenchmark)

	r.Register(&genai.FunctionDeclaration{
		Name:        "check_vulnerabilities",
		Description: "Runs 'govulncheck'.",
	}, checkVulnerabilities)
}

func runTests(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command argument is required")
	}

	// Safety check: block shell metacharacters to prevent command chaining
	if strings.ContainsAny(command, ";|&><`$") {
		return "", fmt.Errorf("security violation: command contains forbidden shell characters")
	}

	// Split command into parts to avoid shell interpretation
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid command")
	}

	baseCmd := parts[0]
	// Safety check: restricted to known test tools
	allowedTools := map[string]bool{
		"go":     true,
		"pytest": true,
		"npm":    true,
		"cargo":  true,
		"make":   true,
	}

	if !allowedTools[baseCmd] && !strings.HasSuffix(baseCmd, "run_tests.sh") {
		return "", fmt.Errorf("security violation: command '%s' is not an authorized test tool", baseCmd)
	}

	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running Tests: %s\033[0m\n", command)
	TerminalMutex.Unlock()

	// Execute the command directly without shell wrapper
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()

	outStr := string(output)
	if err == nil {
		return "PASS", nil
	}

	// If failed, return truncated output to help diagnose
	lines := strings.Split(outStr, "\n")
	if len(lines) > 100 {
		outStr = strings.Join(lines[:100], "\n") + "\n... (Output truncated) ..."
	}

	return fmt.Sprintf("FAIL:\n%s", outStr), nil
}

func goTidy(ctx context.Context, args map[string]interface{}) (string, error) {
	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running go mod tidy and go fmt\033[0m\n")
	TerminalMutex.Unlock()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go mod tidy failed: %s", string(out))
	}

	fmtCmd := exec.CommandContext(ctx, "go", "fmt", "./...")
	if out, err := fmtCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go fmt failed: %s", string(out))
	}

	return "Success: Project tidied and formatted.", nil
}

func getCoverage(ctx context.Context, args map[string]interface{}) (string, error) {
	path := "./..."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Getting test coverage for %s\033[0m\n", path)
	TerminalMutex.Unlock()

	cmd := exec.CommandContext(ctx, "go", "test", "-coverprofile=coverage.out", path)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Sprintf("Tests failed or coverage error:\n%s", string(out)), nil
	}

	// Get summary
	summaryCmd := exec.CommandContext(ctx, "go", "tool", "cover", "-func=coverage.out")
	summaryOut, err := summaryCmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Failed to generate coverage summary: %v", err), nil
	}

	// Clean up
	os.Remove("coverage.out")

	lines := strings.Split(string(summaryOut), "\n")
	if len(lines) > 50 {
		return strings.Join(lines[:50], "\n") + "\n... (truncated)", nil
	}

	return string(summaryOut), nil
}

func runLinter(ctx context.Context, args map[string]interface{}) (string, error) {
	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running linter\033[0m\n")
	TerminalMutex.Unlock()

	// Try golangci-lint first, fallback to staticcheck
	var cmd *exec.Cmd
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		cmd = exec.CommandContext(ctx, "golangci-lint", "run")
	} else if _, err := exec.LookPath("staticcheck"); err == nil {
		cmd = exec.CommandContext(ctx, "staticcheck", "./...")
	} else {
		return "Error: No supported linter found (golangci-lint or staticcheck).", nil
	}

	out, _ := cmd.CombinedOutput()
	if len(out) == 0 {
		return "Linter passed successfully.", nil
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 100 {
		return strings.Join(lines[:100], "\n") + "\n... (truncated)", nil
	}

	return string(out), nil
}

func runBenchmark(ctx context.Context, args map[string]interface{}) (string, error) {
	path := "./..."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}
	bench := "."
	if b, ok := args["bench"].(string); ok && b != "" {
		bench = b
	}

	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running benchmarks (%s) in %s\033[0m\n", bench, path)
	TerminalMutex.Unlock()

	cmd := exec.CommandContext(ctx, "go", "test", "-bench="+bench, "-benchmem", "-run=^$", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Benchmark failed:\n%s", string(out)), nil
	}

	return string(out), nil
}

func checkVulnerabilities(ctx context.Context, args map[string]interface{}) (string, error) {
	TerminalMutex.Lock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Checking for vulnerabilities with govulncheck\033[0m\n")
	TerminalMutex.Unlock()

	if _, err := exec.LookPath("govulncheck"); err != nil {
		return "Error: 'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest", nil
	}

	cmd := exec.CommandContext(ctx, "govulncheck", "./...")
	out, _ := cmd.CombinedOutput()

	if len(out) == 0 {
		return "No vulnerabilities found.", nil
	}

	return string(out), nil
}
