// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPipeCommandsTool(t *testing.T) {
	// 1. Setup Mock Server
	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "pipe_commands",
									"args": {
										"commands": ["echo hello", "grep hello"],
										"reason": "testing piping"
									}
								}
							}]
						}
					}]
				}`)
			} else {
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Piping finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "pipe some commands")
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
	// 1. Setup Mock Server
	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "execute_command",
									"args": {
										"command": "echo redirection",
										"output_file": "out.txt",
										"reason": "testing redirection"
									}
								}
							}]
						}
					}]
				}`)
			} else {
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Redirection finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI
	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "redirect command")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	// 3. Verify file content
	content, err := os.ReadFile(filepath.Join(homeDir, "out.txt"))
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "redirection" {
		t.Errorf("Expected 'redirection' in out.txt, got %q", string(content))
	}
}

func TestPipeCommandsWithRedirectionAndAppend(t *testing.T) {
	homeDir := t.TempDir()
	outFile := filepath.Join(homeDir, "piped_out.txt")

	// Pre-create file with some content for append test
	os.WriteFile(outFile, []byte("initial\n"), 0644)

	// 1. Setup Mock Server
	var turns int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			w.Header().Set("Content-Type", "application/json")
			if turns == 0 {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "pipe_commands",
									"args": {
										"commands": ["echo piped", "cat"],
										"output_file": "piped_out.txt",
										"append": true,
										"reason": "testing piped redirection with append"
									}
								}
							}]
						}
					}]
				}`)
			} else {
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Piped redirection finished."}]}}]}`)
			}
			turns++
			return
		}
	}))
	defer server.Close()

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI
	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "pipe and redirect")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	// 3. Verify file content
	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	expected := "initial\npiped\n"
	if string(content) != expected {
		t.Errorf("Expected %q in piped_out.txt, got %q", expected, string(content))
	}
}
