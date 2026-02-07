// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
	"regexp"
)

type InfoManager struct {
	SP    security.SecurityProvider
	Cache *astutil.ASTCache
}

var genericSkeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^func\s+`),
	regexp.MustCompile(`^type\s+`),
	regexp.MustCompile(`^def\s+`),
	regexp.MustCompile(`^class\s+`),
	regexp.MustCompile(`^function\s+`),
	regexp.MustCompile(`^\w+\(\)\s*\{`),
}

func (m *InfoManager) GetProjectSummary(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var sb strings.Builder
	sb.WriteString("Project Summary:\n")

	// 1. Go Module Info
	if content, err := os.ReadFile("go.mod"); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "go ") {
				sb.WriteString(line + "\n")
			}
		}
	}

	// 2. Stats and Packages
	fileCounts := make(map[string]int)
	packages := make(map[string]bool)
	totalLOC := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext == "" {
			ext = "(no ext)"
		}
		fileCounts[ext]++

		if ext == ".go" {
			packages[filepath.Dir(path)] = true
			// Crude LOC count
			if c, err := os.ReadFile(path); err == nil {
				totalLOC += len(strings.Split(string(c), "\n"))
			}
		}
		return nil
	})

	if err != nil {
		return tools.ToolResult{}, err
	}

	sb.WriteString("\nFile Counts:\n")
	for ext, count := range fileCounts {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", ext, count))
	}

	sb.WriteString(fmt.Sprintf("\nGo Packages (%d):\n", len(packages)))
	for pkg := range packages {
		sb.WriteString(fmt.Sprintf("  - %s\n", pkg))
	}
	sb.WriteString(fmt.Sprintf("\nEstimated Go LOC: %d\n", totalLOC))

	return tools.ToolResult{Text: sb.String()}, nil
}

func (m *InfoManager) GoDoc(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Symbol string `json:"symbol"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	symbol := params.Symbol
	func() {
		m.SP.TerminalLock()
		defer m.SP.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running go doc %s%s\n", colors.ColorCyan, symbol, colors.ColorReset)
	}()

	cmd := exec.CommandContext(ctx, "go", "doc", symbol)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error running go doc: %v\nOutput: %s", err, string(out))}, nil
	}

	return tools.ToolResult{Text: string(out)}, nil
}

func (m *InfoManager) GetFileSkeleton(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Filepath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path, err := m.SP.IsPathSafe(params.Filepath)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if filepath.Ext(path) == ".go" {
		skeleton, err := m.Cache.GetFileSkeletonGo(path)
		if err == nil {
			return tools.ToolResult{Text: skeleton}, nil
		}
		// Fallback to generic if AST parsing fails
	}

	return m.extractGenericSkeleton(path)
}

func (m *InfoManager) extractGenericSkeleton(path string) (tools.ToolResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	var sb strings.Builder
	lines := strings.Split(string(content), "\n")
	var lastComments []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			lastComments = append(lastComments, line)
			continue
		}

		if trimmed == "" {
			lastComments = nil
			continue
		}

		isDef := false
		for _, p := range genericSkeletonPatterns {
			if p.MatchString(line) {
				isDef = true
				break
			}
		}

		if isDef {
			for _, c := range lastComments {
				sb.WriteString(c + "\n")
			}
			sb.WriteString(line + "\n\n")
		}
		lastComments = nil
	}

	res := strings.TrimSpace(sb.String())
	if res == "" {
		return tools.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}
	return tools.ToolResult{Text: res}, nil
}
