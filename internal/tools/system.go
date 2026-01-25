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
	"regexp"
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

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches content from a URL and attempts to clean it into readable documentation by stripping HTML tags and boilerplate.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, readExternalDocs)
}

func askUser(args map[string]interface{}) (string, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return "", fmt.Errorf("question argument is required")
	}

	// Tell-me style: Question in magenta, followed by "Answer > " prompt
	fmt.Fprintf(os.Stderr, "\033[1;35m[AI Question] %s\033[0m\n", question)
	fmt.Fprintf(os.Stderr, "Answer > ")

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

func readExternalDocs(args map[string]interface{}) (string, error) {
	content, err := readURL(args)
	if err != nil {
		return "", err
	}

	// Basic HTML stripping
	// 1. Remove script and style tags and their contents
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	content = reStyle.ReplaceAllString(content, "")
	content = reScript.ReplaceAllString(content, "")

	// 2. Remove all other HTML tags
	reTags := regexp.MustCompile(`<.*?>`)
	content = reTags.ReplaceAllString(content, " ")

	// 3. Clean up whitespace
	reSpace := regexp.MustCompile(`\n\s*\n`)
	content = reSpace.ReplaceAllString(content, "\n\n")
	content = strings.Join(strings.Fields(content), " ")

	// Truncate to avoid huge inputs
	if len(content) > 10000 {
		content = content[:10000] + "\n... (truncated)"
	}

	return content, nil
}

func isSafeCommand(command string) bool {
	safeCommands := `^(grep|ls|pwd|cat|echo|head|tail|wc|stat|date|whoami|diff|awk|sed)`
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	base := parts[0]

	// 1. Check against whitelist
	match, _ := regexp.MatchString(safeCommands, base)
	if !match {
		return false
	}

	// 2. Check for unsafe characters (pipes, redirects, etc.)
	unsafeChars := []string{"|", "&", ";", ">", "<", "$(", "`"}
	for _, char := range unsafeChars {
		if strings.Contains(command, char) {
			return false
		}
	}

	return true
}

func readSingleKey() (string, error) {
	// Disable input buffering
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	// Restore input buffering on exit
	defer exec.Command("stty", "-F", "/dev/tty", "-cbreak").Run()

	var b []byte = make([]byte, 1)
	_, err := os.Stdin.Read(b)
	if err != nil {
		return "", err
	}
	return strings.ToLower(string(b)), nil
}

func executeCommand(args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command argument is required")
	}

	reason, _ := args["reason"].(string)

	approved := false

	// 1. Check for Auto-Approval (Safe read-only commands)
	if isSafeCommand(command) {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe read-only command detected.\033[0m\n")
		approved = true
	} else {
		// 2. Safety Confirmation Gate (Tell-me style)
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Command: %s\033[0m\n", command)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this command? (y/N) ")

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n") // New line after key hit

		if err == nil && (char == "y") {
			approved = true
		}
	}

	if !approved {
		return fmt.Sprintf("User denied execution of command: %s", command), nil
	}

	// 3. Execution
	fmt.Fprintf(os.Stderr, "\033[0;33mExecuting... (Output shown below)\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	// We use "sh -c" to allow for complex commands
	cmd := exec.Command("sh", "-c", command)

	// Stream output to stderr and capture it
	var sb strings.Builder
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	multi := io.MultiReader(stdout, stderr)

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Command failed to start: %v", err), nil
	}

	scanner := bufio.NewScanner(multi)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(os.Stderr, "  \033[90m%s\033[0m\n", line)
		sb.WriteString(line + "\n")
	}

	err := cmd.Wait()
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	output := sb.String()
	if err != nil {
		return fmt.Sprintf("Exit Code: 1\nError/Output:\n%s", output), nil
	}

	return fmt.Sprintf("Exit Code: 0\nOutput:\n%s", output), nil
}
