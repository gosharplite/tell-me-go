// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/astutil"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func (m *fileSystemManager) getFileSkeleton(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	if path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	if filepath.Ext(resolvedPath) == ".go" {
		skeleton, err := astutil.GetFileSkeletonGo(resolvedPath)
		if err == nil {
			return types.ToolResult{Text: skeleton}, nil
		}
		// Fallback to heuristic if AST fails
	}

	file, err := m.fs.Open(ctx, resolvedPath)
	if err != nil {
		return types.ToolResult{}, err
	}
	defer file.Close()

	ext := filepath.Ext(path)
	scanner := bufio.NewScanner(file)
	var sb strings.Builder
	var lastComments []string

	// Simple heuristic: extract lines that look like definitions and their preceding comments
	defPatterns := []string{
		`^func\s+`, `^type\s+`, `^def\s+`, `^class\s+`, `^function\s+`, `^\w+\(\)\s*\{`,
	}

	for scanner.Scan() {
		line := scanner.Text()
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
		for _, p := range defPatterns {
			if matched, _ := regexp.MatchString(p, line); matched {
				isDef = true
				break
			}
		}

		if isDef {
			for _, c := range lastComments {
				sb.WriteString(c + "\n")
			}
			sb.WriteString(line + "\n")
			if ext == ".py" && strings.HasSuffix(trimmed, ":") {
				// Keep going for Python
			} else if !strings.HasSuffix(trimmed, "{") && ext != ".py" {
				// Might be a multi-line signature or type, but we keep it simple
			}
			sb.WriteString("\n")
		}
		lastComments = nil
	}

	out := sb.String()
	if out == "" {
		return types.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}

	return types.ToolResult{Text: out}, nil
}
