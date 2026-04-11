// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPipeCommandsTool(t *testing.T) {
	t.Parallel()
	// 1. Setup Mock Server

	echoCmd := "echo"
	grepCmd := "grep"
	if runtime.GOOS == "windows" {
		echoCmd = "cmd /c echo"
		grepCmd = "findstr"
	}

	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				_, _ = fmt.Fprintf(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "pipe_commands",
									"args": {
										"commands": [%q, %q],
										"reason": "testing piping"
									}
								}
							}]
						}
					}]
				}`, echoCmd+" hello", grepCmd+" hello")
			} else {
				_, _ = fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Piping finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_MOCK_ANSWER=y",
	}

	// 2. Run CLI
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "pipe some commands")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 3. Verification
	errOut := stripANSI(stderr)
	if !strings.Contains(errOut, "Executing Pipeline...") {
		t.Errorf("Expected pipeline execution log in stderr, got: %q", errOut)
	}
	if !strings.Contains(errOut, "hello") {
		t.Errorf("Expected 'hello' in pipeline output in stderr, got: %q", errOut)
	}
}

func TestExecuteCommandWithRedirection(t *testing.T) {
	t.Parallel()
	// 1. Setup Mock Server

	echoCmd := "echo"
	if runtime.GOOS == "windows" {
		echoCmd = "cmd /c echo"
	}

	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				_, _ = fmt.Fprintf(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "execute_command",
									"args": {
										"command": %q,
										"output_file": "out.txt",
										"reason": "testing redirection"
									}
								}
							}]
						}
					}]
				}`, echoCmd+" redirection")
			} else {
				_, _ = fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Redirection finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_MOCK_ANSWER=y",
	}

	// 2. Run CLI
	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "redirect command")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	// 3. Verify file content
	content, err := os.ReadFile(filepath.Join(homeDir, "out.txt"))
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	// Windows echo adds \r\n and sometimes a space.
	got := strings.TrimSpace(string(content))
	if got != "redirection" {
		t.Errorf("Expected 'redirection' in out.txt, got %q", string(content))
	}
}

func TestPipeCommandsWithRedirectionAndAppend(t *testing.T) {
	t.Parallel()
	homeDir := t.TempDir()
	outFile := filepath.Join(homeDir, "piped_out.txt")

	// Pre-create file with some content for append test
	if err := os.WriteFile(outFile, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	echoCmd := "echo"
	catCmd := "cat"
	if runtime.GOOS == "windows" {
		echoCmd = "cmd /c echo"
		// 'findstr ^' is a common trick for cat-like behavior on Windows.
		catCmd = "findstr ^"
	}

	// 1. Setup Mock Server
	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				_, _ = fmt.Fprintf(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "pipe_commands",
									"args": {
										"commands": [%q, %q],
										"output_file": "piped_out.txt",
										"append": true,
										"reason": "testing piped redirection with append"
									}
								}
							}]
						}
					}]
				}`, echoCmd+" piped", catCmd)
			} else {
				_, _ = fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Piped redirection finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_MOCK_ANSWER=y",
	}

	// 2. Run CLI
	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "pipe and redirect")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	// 3. Verify file content
	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	// Normalize line endings for comparison
	got := strings.ReplaceAll(string(content), "\r\n", "\n")
	// Trim trailing whitespace from each line to handle Windows echo behavior
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n")

	expected := "initial\npiped\n"
	if got != expected {
		t.Errorf("Expected %q in piped_out.txt, got %q", expected, got)
	}
}
