// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"google.golang.org/genai"
)

// RegisterSystemTools adds system-related tools to the registry.
func RegisterSystemTools(r *Registry) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "execute_command",
		Description: "Executes a shell command on the local system. Requires user confirmation for safety.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"command": {
					Type:        genai.TypeString,
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
				"reason": {
					Type:        genai.TypeString,
					Description: "A short explanation of why this command needs to be executed.",
				},
			},
			Required: []string{"command"},
		},
	}, executeCommand)

	r.Register(&genai.FunctionDeclaration{
		Name:        "ask_user",
		Description: "Asks the user a specific question to clarify requirements or request confirmation. Use this when you need input before proceeding.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"question": {
					Type:        genai.TypeString,
					Description: "The question to ask the user.",
				},
			},
			Required: []string{"question"},
		},
	}, askUser)

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_url",
		Description: "Fetches the content of a specific URL. Useful for reading documentation or articles.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "The URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, readURL)
}

func askUser(args map[string]interface{}) (string, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return "", fmt.Errorf("question argument is required")
	}

	fmt.Fprintf(os.Stderr, "\n\033[1;35m[Question]\033[0m %s\n", question)
	fmt.Fprintf(os.Stderr, "Response: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read user response: %w", err)
	}

	return strings.TrimSpace(response), nil
}

func readURL(args map[string]interface{}) (string, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("url argument is required")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}

func executeCommand(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command argument is required")
	}

	reason, _ := args["reason"].(string)

	// 1. Safety Confirmation Gate
	fmt.Fprintf(os.Stderr, "\n\033[1;33m[Confirmation Required]\033[0m\n")
	if reason != "" {
		fmt.Fprintf(os.Stderr, "Reason: %s\n", reason)
	}
	fmt.Fprintf(os.Stderr, "Command: \033[1;36m%s\033[0m\n", command)
	fmt.Fprintf(os.Stderr, "Allow execution? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read user input: %w", err)
	}

	input = strings.ToLower(strings.TrimSpace(input))
	if input != "y" && input != "yes" {
		return "Operation cancelled by user.", nil
	}

	// 2. Execution
	// We use "sh -c" to allow for complex commands (pipes, redirects) similar to the Bash version.
	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Return both error and output so the model can see what went wrong.
		return fmt.Sprintf("Command failed with error: %v\nOutput:\n%s", err, string(output)), nil
	}

	return string(output), nil
}
