// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/astutil"
	"github.com/gosharplite/tell-me-go/internal/types"
)

var (
	skeletonPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^func\s+`),
		regexp.MustCompile(`^type\s+`),
		regexp.MustCompile(`^def\s+`),
		regexp.MustCompile(`^class\s+`),
		regexp.MustCompile(`^function\s+`),
		regexp.MustCompile(`^\w+\(\)\s*\{`),
	}
)

func (m *fileSystemManager) getFileSkeleton(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	// 1. Parse Arguments
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}
	if params.FilePath == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	// 2. Security Check
	resolvedPath, err := m.sm.IsPathSafe(params.FilePath)
	if err != nil {
		return types.ToolResult{}, err
	}

	// 3. Primary Strategy: Go AST
	if filepath.Ext(resolvedPath) == ".go" {
		if skeleton, err := astutil.GetFileSkeletonGo(resolvedPath); err == nil {
			return types.ToolResult{Text: skeleton}, nil
		}
	}

	// 4. Fallback Strategy: Generic Heuristic
	return m.extractGenericSkeleton(ctx, resolvedPath)
}

func (m *fileSystemManager) extractGenericSkeleton(ctx context.Context, path string) (types.ToolResult, error) {
	file, err := m.fs.Open(ctx, path)
	if err != nil {
		return types.ToolResult{}, err
	}
	defer file.Close()

	ext := filepath.Ext(path)
	skeleton, err := scanForDefinitions(file, ext)
	if err != nil {
		return types.ToolResult{}, err
	}

	if skeleton == "" {
		return types.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}
	return types.ToolResult{Text: skeleton}, nil
}

func scanForDefinitions(r io.Reader, ext string) (string, error) {
	scanner := bufio.NewScanner(r)
	var sb strings.Builder
	var lastComments []string

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
		for _, p := range skeletonPatterns {
			if p.MatchString(line) {
				isDef = true
				break
			}
		}

		if isDef {
			for _, c := range lastComments {
				sb.WriteString(c + "\n")
			}
			sb.WriteString(line + "\n")
			sb.WriteString("\n")
		}
		lastComments = nil
	}
	return sb.String(), scanner.Err()
}
