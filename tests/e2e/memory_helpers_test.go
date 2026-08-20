// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildFakePlur compiles the stdio fake PLUR MCP server
// (tests/e2e/testdata/fakeplur) into a per-test temp directory and returns
// the binary path. A per-test build of a tiny stdlib-only package is
// acceptable; this deliberately does not touch e2e_test.go's TestMain.
func buildFakePlur(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakeplur")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./tests/e2e/testdata/fakeplur")
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build fake plur server: %v\nOutput: %s", err, out)
	}
	return bin
}

// writeSeedFile writes a JSON array of out-of-band pinned engrams to path
// and returns the path. The seed format is the documented no-pin-tool E2E
// setup step (ADR-068 §13): [{"id","text","keywords",...,"pinned":true}].
func writeSeedFile(t *testing.T, path string, engrams []map[string]interface{}) string {
	t.Helper()
	data, err := json.Marshal(engrams)
	if err != nil {
		t.Fatalf("failed to marshal seed engrams: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write seed file: %v", err)
	}
	return path
}

// memoryE2EConfigWithEnabled writes a config identical to memoryE2EConfig
// except that MEMORY.ENABLED takes the given value (issue #1414 — the
// disabled-config E2E leg needs ENABLED: false; every pre-existing caller
// keeps ENABLED: true via memoryE2EConfig).
func memoryE2EConfigWithEnabled(t *testing.T, mockURL, fakePlurPath string, learn string, enabled bool) string {
	t.Helper()
	content := fmt.Sprintf(`
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "openai"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
MCP_SERVERS:
  plur:
    COMMAND: '%s'
MEMORY:
  ENABLED: %t
  SERVER: "plur"
  INJECT_BUDGET: 2000
  LEARN: "%s"
`, mockURL, fakePlurPath, enabled, learn)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write memory config: %v", err)
	}
	return path
}

// memoryE2EConfig writes a config file that mirrors createTempConfig's shape
// (provider openai, SELECTED_PROVIDER mock) plus the memory integration
// surface: MCP_SERVERS.plur pointing at the fake binary and MEMORY enabled
// with the given LEARN tier. Returns the config path.
func memoryE2EConfig(t *testing.T, mockURL, fakePlurPath string, learn string) string {
	t.Helper()
	return memoryE2EConfigWithEnabled(t, mockURL, fakePlurPath, learn, true)
}

// memoryEnv builds the standard environment for a memory E2E CLI run:
// TELL_ME_HOME (the persona home), HOME (a temp dir, keeping the advisory
// flock lock file out of the real home), TELL_ME_DEBUG=1 (surfaces the
// Info-level memory_injected log on stderr — the CLI's default level is
// Warn), FAKE_PLUR_STORE, TELL_ME_MOCK_URL, and FAKE_PLUR_SEED when given.
// The harness filters TELL_ME_* from the parent env but passes this slice
// through to the CLI child, which forwards its own env to the stdio-spawned
// fake.
func memoryEnv(t *testing.T, homeDir, mockURL, storePath, seedPath string) []string {
	t.Helper()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"HOME=" + filepath.Join(t.TempDir(), "home"),
		"TELL_ME_DEBUG=1",
		"FAKE_PLUR_STORE=" + storePath,
		"TELL_ME_MOCK_URL=" + mockURL,
	}
	if seedPath != "" {
		env = append(env, "FAKE_PLUR_SEED="+seedPath)
	}
	return env
}

// runMemoryCLI runs the CLI once against the memory config with a fresh
// session. The caller-supplied env must include TELL_ME_HOME,
// FAKE_PLUR_STORE and TELL_ME_MOCK_URL (see memoryEnv); FAKE_PLUR_SEED is
// optional.
func runMemoryCLI(t *testing.T, homeDir string, env []string, configPath, prompt string) (stdout, stderr string, err error) {
	t.Helper()
	return runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "--new", prompt)
}

// validateMemoryInjection asserts that an intercepted LLM request carries
// the ## PLUR MEMORY block with the given engram id, and that the block
// lives in a system/developer message.
func validateMemoryInjection(t *testing.T, interceptedRequest, wantID string) {
	t.Helper()
	if interceptedRequest == "" {
		t.Fatal("no request intercepted by mock server")
	}
	assertContains(t, interceptedRequest, "## PLUR MEMORY")
	assertContains(t, interceptedRequest, wantID)

	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(interceptedRequest), &req); err != nil {
		t.Fatalf("failed to unmarshal intercepted request: %v", err)
	}
	found := false
	for _, msg := range req.Messages {
		if (msg.Role == "system" || msg.Role == "developer") &&
			strings.Contains(msg.Content, "## PLUR MEMORY") &&
			strings.Contains(msg.Content, wantID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("memory block with id %q not found in a system/developer message. Request: %s", wantID, interceptedRequest)
	}
}

// plurStoreFile is the on-disk shape of the fake's store
// (tests/e2e/testdata/fakeplur/main.go).
type plurStoreFile struct {
	Engrams []struct {
		ID       string   `json:"id"`
		Text     string   `json:"text"`
		Keywords []string `json:"keywords"`
		Pinned   bool     `json:"pinned"`
	} `json:"engrams"`
	Episodes []map[string]interface{} `json:"episodes"`
}

// readPlurStore reads and parses the fake's store file.
func readPlurStore(t *testing.T, path string) plurStoreFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read plur store %s: %v", path, err)
	}
	var st plurStoreFile
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("failed to parse plur store %s: %v", path, err)
	}
	return st
}

// findEngramByText returns the id of the first engram whose text exactly
// matches, plus ok=false when none does.
func findEngramByText(st plurStoreFile, text string) (id string, ok bool) {
	for _, e := range st.Engrams {
		if e.Text == text {
			return e.ID, true
		}
	}
	return "", false
}
