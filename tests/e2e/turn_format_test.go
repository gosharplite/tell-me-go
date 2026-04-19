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

// assertHeaderFormat verifies the visual structure of the CLI output for a single turn.
func assertHeaderFormat(t *testing.T, errOut string, mode string) {
	t.Helper()
	lines := strings.Split(errOut, "\n")

	// Define patterns to look for in the output
	patterns := []struct {
		name  string
		regex *regexp.Regexp
		match string
	}{
		{
			name:  "horizontal_rule",
			match: "────────────────────────────────────────────────────────────────────────────────",
		},
		{
			name:  "turn_line",
			match: "╭─⠿ Turn 1 - " + mode,
		},
		{
			name:  "estimated_payload",
			regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: ~\d+/\d+ tokens - ` + mode + `$`),
		},
		{
			name:  "actual_payload",
			regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] Payload: \d+/\d+ tokens - ` + mode + `$`),
		},
		{
			name: "metrics_line",
			// Th: segment is optional in the rendered metrics line: it is
			// suppressed when ThinkingTokens == 0 (notably for Anthropic,
			// which does not separately report reasoning tokens, and for
			// any provider on a non-reasoning turn). When present, the
			// segment is " Th: <n>". See issue #72.
			regex: regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] \[[^\]]+\] M: \d+ H: \d+ C: \d+( Th: \d+)?.*\[.*\]$`),
		},
		{
			name:  "ready_line",
			match: "╰─⠿ Ready",
		},
	}

	// Find indices of each pattern
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

	// 1. Assert Presence
	for _, p := range patterns {
		if indices[p.name] == -1 {
			t.Errorf("%s not found in stderr. (mode: %s)\nFull Output:\n%s", p.name, mode, errOut)
		}
	}

	// 2. Assert Formatting specifics
	if hIdx := indices["horizontal_rule"]; hIdx != -1 {
		// Rule should be exactly the expected string
		if lines[hIdx] != patterns[0].match {
			t.Errorf("Horizontal rule mismatch.\nExpected: %q\nGot: %q", patterns[0].match, lines[hIdx])
		}
		// Rule should be preceded by an empty line (unless it's the very first line)
		if hIdx > 0 && strings.TrimSpace(lines[hIdx-1]) != "" {
			t.Errorf("Horizontal rule should be preceded by an empty line, but preceding line is: %q", lines[hIdx-1])
		}
	}

	if mIdx := indices["metrics_line"]; mIdx != -1 {
		metricsLine := lines[mIdx]
		// Th: is intentionally absent from the required-component list.
		// The renderer suppresses " Th: <n>" when ThinkingTokens == 0
		// (issue #72). The metrics_line regex above tolerates either
		// presence or absence; presence-when-non-zero is pinned by
		// internal/ui/renderer_test.go::TestRenderMetricsLine_ThinkingSegmentSuppression.
		required := []string{" M: ", " H: ", " C: ", " ["}
		for _, r := range required {
			if !strings.Contains(metricsLine, r) {
				t.Errorf("Metrics line missing required component %q: %q", r, metricsLine)
			}
		}
	}

	// 3. Assert Order
	// Expected sequence: horizontal_rule -> turn_line -> estimated_payload -> actual_payload -> metrics_line -> ready_line
	order := []string{"horizontal_rule", "turn_line", "estimated_payload", "actual_payload", "metrics_line", "ready_line"}
	for i := 0; i < len(order)-1; i++ {
		idx1 := indices[order[i]]
		idx2 := indices[order[i+1]]
		if idx1 != -1 && idx2 != -1 && idx1 >= idx2 {
			t.Errorf("%s (idx %d) should appear before %s (idx %d)", order[i], idx1, order[i+1], idx2)
		}
	}
}
