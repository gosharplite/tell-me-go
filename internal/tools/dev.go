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
