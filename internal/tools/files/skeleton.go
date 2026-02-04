// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type fileSkeleton struct {
	sm *security.SecurityManager
	fs fsutil.FileSystem
}

var skeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^func\s+`),
	regexp.MustCompile(`^type\s+`),
	regexp.MustCompile(`^def\s+`),
	regexp.MustCompile(`^class\s+`),
	regexp.MustCompile(`^function\s+`),
	regexp.MustCompile(`^\w+\(\)\s*\{`),
}

func (s *fileSkeleton) getFileSkeleton(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}
	if params.FilePath == "" {
		return tools.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := s.sm.IsPathSafe(params.FilePath)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return s.extractGenericSkeleton(ctx, resolvedPath)
}

func (s *fileSkeleton) extractGenericSkeleton(ctx context.Context, path string) (tools.ToolResult, error) {
	file, err := s.fs.Open(ctx, path)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer file.Close()

	ext := filepath.Ext(path)
	skeleton, err := scanForDefinitions(file, ext)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if skeleton == "" {
		return tools.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}
	return tools.ToolResult{Text: skeleton}, nil
}

func scanForDefinitions(r io.Reader, ext string) (string, error) {
	const maxScannerCapacity = 10 * 1024 * 1024
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)

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
