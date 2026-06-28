// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"regexp"
	"strings"
	"testing"
)

func TestTurnHeaderFormat(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	tests := []struct {
		name     string
		provider string
		mode     string
	}{
		{
			name:     "google_architect_header_format",
			provider: "google",
			mode:     "architect",
		},
		{
			name:     "openai_coder_header_format",
			provider: "openai",
			mode:     "coder",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock LLM server that returns a simple text response
			server := setupSimpleTextMockServer(t, tt.provider)
			defer server.Close()

			homeDir := t.TempDir()
			configPath := createTempConfigWithMode(t, tt.provider, server.URL, tt.mode)
			env := []string{
				"TELL_ME_HOME=" + homeDir,
				"TELL_ME_MOCK_URL=" + server.URL,
			}

			// Run the CLI with a simple prompt.
			_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "Say hello")
			if err != nil {
				t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
			}

			// Capture stderr and strip ANSI codes.
			assertHeaderFormat(t, stripANSI(stderr), tt.mode)
		})
	}
}

// headerPattern defines a named pattern to search for in CLI output.
type headerPattern struct {
	name  string
	regex *regexp.Regexp
	match string
}

// findHeaderPatternIndices scans lines for each pattern and returns their line indices.
// Patterns already found are skipped (first-match-wins).
func findHeaderPatternIndices(t *testing.T, lines []string, patterns []headerPattern) map[string]int {
	t.Helper()
	indices := make(map[string]int)
	for _, p := range patterns {
		indices[p.name] = -1
	}
	for i, line := range lines {
		for _, p := range patterns {
			if indices[p.name] != -1 {
				continue
			}
			if p.regex != nil && p.regex.MatchString(line) {
				indices[p.name] = i
			} else if p.match != "" && strings.HasPrefix(line, p.match) {
				indices[p.name] = i
			}
		}
	}
	return indices
}

// assertHeaderOrder verifies that the expected pattern names appear in sequence.
func assertHeaderOrder(t *testing.T, indices map[string]int, order []string) {
	t.Helper()
	for i := 0; i < len(order)-1; i++ {
		idx1 := indices[order[i]]
		idx2 := indices[order[i+1]]
		if idx1 != -1 && idx2 != -1 && idx1 >= idx2 {
			t.Errorf("%s (idx %d) should appear before %s (idx %d)", order[i], idx1, order[i+1], idx2)
		}
	}
}

// assertHeaderFormat verifies the visual structure of the CLI output for a single turn.
func assertHeaderFormat(t *testing.T, errOut string, mode string) {
	t.Helper()
	lines := strings.Split(errOut, "\n")

	patterns := []headerPattern{
		{name: "horizontal_rule", match: "────────────────────────────────────────────────────────────────────────────────"},
		{name: "turn_line", match: "╭─⠿ Turn 1 - " + mode},
		{name: "estimated_payload", regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: ~\d+/\d+ tokens - ` + mode + `( - [\w.-]+)?$`)},
		{name: "actual_payload", regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: \d+/\d+ tokens - ` + mode + `( - [\w.-]+)?$`)},
		{name: "metrics_line", regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] \[[^\]]+\] M: \d+ H: \d+ C: \d+( Th: \d+)?.*\[.*\]$`)},
		{name: "ready_line", match: "╰─⠿ Ready"},
	}

	indices := findHeaderPatternIndices(t, lines, patterns)

	// Assert presence
	for _, p := range patterns {
		if indices[p.name] == -1 {
			t.Errorf("%s not found in stderr. (mode: %s)\nFull Output:\n%s", p.name, mode, errOut)
		}
	}

	// Assert horizontal rule specifics
	if hIdx := indices["horizontal_rule"]; hIdx != -1 {
		if lines[hIdx] != patterns[0].match {
			t.Errorf("Horizontal rule mismatch.\nExpected: %q\nGot: %q", patterns[0].match, lines[hIdx])
		}
		if hIdx > 0 && strings.TrimSpace(lines[hIdx-1]) != "" {
			t.Errorf("Horizontal rule should be preceded by an empty line, but preceding line is: %q", lines[hIdx-1])
		}
	}

	// Assert metrics line components
	if mIdx := indices["metrics_line"]; mIdx != -1 {
		metricsLine := lines[mIdx]
		for _, r := range []string{" M: ", " H: ", " C: ", " ["} {
			if !strings.Contains(metricsLine, r) {
				t.Errorf("Metrics line missing required component %q: %q", r, metricsLine)
			}
		}
	}

	// Assert order
	assertHeaderOrder(t, indices, []string{
		"horizontal_rule", "turn_line", "estimated_payload",
		"actual_payload", "metrics_line", "ready_line",
	})
}
