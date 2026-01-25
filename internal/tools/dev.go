// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"google.golang.org/genai"
)

// RegisterDevTools adds developer-related tools to the registry.
func RegisterDevTools(r *Registry) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "run_tests",
		Description: "Executes project tests (Go, Python, NPM, etc.) with automatic output truncation.",
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
	}, runTests)

	r.Register(&genai.FunctionDeclaration{
		Name:        "go_tidy",
		Description: "Runs 'go mod tidy' and 'go fmt ./...' to clean up dependencies and format code.",
	}, goTidy)

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
}

func runTests(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command argument is required")
	}

	// Safety check: restricted to known test patterns
	safeTestPatterns := `^(\./.*run_tests\.sh|pytest|npm\s+test|go\s+test|cargo\s+test|make\s+test)`
	matched, _ := regexp.MatchString(safeTestPatterns, command)
	if !matched {
		return "", fmt.Errorf("security violation: command '%s' is not a recognized test command", command)
	}

	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running Tests: %s\033[0m\n", command)

	// Execute the command
	cmd := exec.Command("sh", "-c", command)
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

func goTidy(args map[string]interface{}) (string, error) {
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running go mod tidy and go fmt\033[0m\n")

	tidyCmd := exec.Command("go", "mod", "tidy")
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go mod tidy failed: %s", string(out))
	}

	fmtCmd := exec.Command("go", "fmt", "./...")
	if out, err := fmtCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go fmt failed: %s", string(out))
	}

	return "Success: Project tidied and formatted.", nil
}

func getCoverage(args map[string]interface{}) (string, error) {
	path := "./..."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Getting test coverage for %s\033[0m\n", path)

	cmd := exec.Command("go", "test", "-coverprofile=coverage.out", path)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Sprintf("Tests failed or coverage error:\n%s", string(out)), nil
	}

	// Get summary
	summaryCmd := exec.Command("go", "tool", "cover", "-func=coverage.out")
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

func runLinter(args map[string]interface{}) (string, error) {
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running linter\033[0m\n")

	// Try golangci-lint first, fallback to staticcheck
	var cmd *exec.Cmd
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		cmd = exec.Command("golangci-lint", "run")
	} else if _, err := exec.LookPath("staticcheck"); err == nil {
		cmd = exec.Command("staticcheck", "./...")
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
