// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

type InfoManager struct {
	SP    security.SecurityProvider
	Cache *ASTCache
	FS    storage.FileSystem
}

var genericSkeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(export\s+)?(async\s+)?func(tion)?\s+`),
	regexp.MustCompile(`^(export\s+)?(async\s+)?class\s+`),
	regexp.MustCompile(`^(export\s+)?(type|def)\s+`),
	regexp.MustCompile(`^(export\s+)?\w+\(\)\s*\{`),
	regexp.MustCompile(`^package\s+`),
}

func (m *InfoManager) GetProjectSummary(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	modInfo := m.resolveModuleInfo(ctx)
	fileCounts, packages, totalLOC, err := m.collectFileStats(ctx)
	if err != nil {
		return tools.ToolResult{}, err
	}
	summary := m.renderProjectSummary(modInfo, fileCounts, packages, totalLOC)
	return tools.ToolResult{Text: summary}, nil
}

func (m *InfoManager) resolveModuleInfo(ctx context.Context) string {
	var sb strings.Builder
	if content, err := m.FS.ReadFile(ctx, "go.mod"); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "go ") {
				sb.WriteString(line + "\n")
			}
		}
	}
	return sb.String()
}

func (m *InfoManager) collectFileStats(ctx context.Context) (map[string]int, map[string]bool, int, error) {
	fileCounts := make(map[string]int)
	packages := make(map[string]bool)
	totalLOC := 0

	err := m.FS.Walk(ctx, ".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
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
			if c, err := m.FS.ReadFile(ctx, path); err == nil {
				totalLOC += bytes.Count(c, []byte("\n"))
				if len(c) > 0 && c[len(c)-1] != '\n' {
					totalLOC++
				}
			}
		}
		return nil
	})

	return fileCounts, packages, totalLOC, err
}

func (m *InfoManager) renderProjectSummary(modInfo string, fileCounts map[string]int, packages map[string]bool, totalLOC int) string {
	var sb strings.Builder
	sb.WriteString("Project Summary:\n")
	sb.WriteString(modInfo)

	sb.WriteString("\nFile Counts:\n")
	exts := make([]string, 0, len(fileCounts))
	for ext := range fileCounts {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	for _, ext := range exts {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", ext, fileCounts[ext]))
	}

	sb.WriteString(fmt.Sprintf("\nGo Packages (%d):\n", len(packages)))
	pkgs := make([]string, 0, len(packages))
	for pkg := range packages {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		sb.WriteString(fmt.Sprintf("  - %s\n", pkg))
	}
	sb.WriteString(fmt.Sprintf("\nEstimated Go LOC: %d\n", totalLOC))

	return sb.String()
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
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running go doc %s%s\n", ui.ColorCyan, symbol, ui.ColorReset)
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

	return m.extractGenericSkeleton(ctx, path)
}

func (m *InfoManager) extractGenericSkeleton(ctx context.Context, path string) (tools.ToolResult, error) {
	content, err := m.FS.ReadFile(ctx, path)
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
