// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type infoManager struct {
	SP     domain_security.PathValidator
	Cache  *astCache
	FS     persistence.FileSystem
	Events events.EventBus
	Runner AnalysisGoRunner
}

type projectStats struct {
	fileCounts map[string]int
	packages   map[string]bool
	totalLOC   int
}

func (m *infoManager) isIgnoredDir(name string, isDir bool) bool {
	if !isDir {
		return false
	}
	// Skip hidden directories, testdata, vendor, and node_modules
	return name != "." && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules")
}

func (m *infoManager) isTargetSourceFile(name string, isDir bool) bool {
	if isDir {
		return false
	}
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func (m *infoManager) recordGoStats(ctx context.Context, path string, info os.FileInfo, stats *projectStats) {
	// Normalize package paths to use forward slashes for consistent test output across OSes
	stats.packages[filepath.ToSlash(filepath.Dir(path))] = true

	// Try cache first to avoid redundant disk I/O
	var loc int
	var ok bool
	if m.Cache != nil {
		// path is from filepath.Walk, rooted at "." — matches cache.baseDir.
		// absPath resolves it to absolute internally.
		loc, ok = m.Cache.GetCachedLineCount(path, info)
	}

	if ok {
		stats.totalLOC += loc
	} else if loc, err := m.countLines(ctx, path); err == nil {
		stats.totalLOC += loc
	}
}

func (m *infoManager) sendHeartbeat(hb chan<- struct{}, count int) {
	if count%50 == 0 && hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}
}

func (m *infoManager) getFileExtension(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return "(no ext)"
	}
	return ext
}

func (m *infoManager) makeWalkFunc(ctx context.Context, stats *projectStats, hb chan<- struct{}) persistence.WalkFunc {
	count := 0
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		name := info.Name()
		isDir := info.IsDir()
		if m.isIgnoredDir(name, isDir) {
			return filepath.SkipDir
		}

		if isDir {
			return nil
		}

		count++
		m.sendHeartbeat(hb, count)

		ext := m.getFileExtension(path)
		stats.fileCounts[ext]++

		if m.isTargetSourceFile(name, isDir) {
			m.recordGoStats(ctx, path, info, stats)
		}
		return nil
	}
}

var genericSkeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(export\s+)?(async\s+)?func(tion)?\s+`),
	regexp.MustCompile(`^(export\s+)?(async\s+)?class\s+`),
	regexp.MustCompile(`^(export\s+)?(type|def)\s+`),
	regexp.MustCompile(`^(export\s+)?\w+\(\)\s*\{`),
	regexp.MustCompile(`^package\s+`),
}

func (m *infoManager) GetProjectSummary(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	modInfo := m.resolveModuleInfo(ctx)
	fileCounts, packages, totalLOC, err := m.collectFileStats(ctx, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	summary := m.renderProjectSummary(modInfo, fileCounts, packages, totalLOC)
	return tools.ToolResult{Text: summary}, nil
}

func (m *infoManager) resolveModuleInfo(ctx context.Context) string {
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

func (m *infoManager) collectFileStats(ctx context.Context, hb chan<- struct{}) (map[string]int, map[string]bool, int, error) {
	stats := &projectStats{
		fileCounts: make(map[string]int),
		packages:   make(map[string]bool),
	}

	err := m.FS.Walk(ctx, ".", m.makeWalkFunc(ctx, stats, hb))
	return stats.fileCounts, stats.packages, stats.totalLOC, err
}

func (m *infoManager) renderProjectSummary(modInfo string, fileCounts map[string]int, packages map[string]bool, totalLOC int) string {
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
		_, _ = fmt.Fprintf(&sb, "  %s: %d\n", ext, fileCounts[ext])
	}

	_, _ = fmt.Fprintf(&sb, "\nGo Packages (%d):\n", len(packages))
	pkgs := make([]string, 0, len(packages))
	for pkg := range packages {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		_, _ = fmt.Fprintf(&sb, "  - %s\n", pkg)
	}
	_, _ = fmt.Fprintf(&sb, "\nEstimated Go LOC: %d\n", totalLOC)

	return sb.String()
}

func (m *infoManager) GoDoc(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Symbol string `json:"symbol"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	symbol := params.Symbol
	m.publishGoDocEvent(ctx, symbol)

	// Heartbeat while running go doc
	done := m.startGoDocHeartbeat(hb)

	out, err := m.Runner.GetGoDoc(ctx, symbol)
	close(done)

	return m.formatDocOutput(out, err), nil
}

func (m *infoManager) publishGoDocEvent(ctx context.Context, symbol string) {
	if m.Events == nil {
		return
	}
	evt := events.SystemMessageEvent{
		Message: fmt.Sprintf("[Tool Action] Running go doc %s", symbol),
		Level:   "info",
	}
	if err := events.SafePublish(ctx, m.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			slog.Default().Error("event_publish_failed",
				slog.String("event_type", string(evt.Type())),
				slog.Any("error", err))
		}
	}
}

func (m *infoManager) startGoDocHeartbeat(hb chan<- struct{}) chan struct{} {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return done
}

func (m *infoManager) formatDocOutput(out []byte, err error) tools.ToolResult {
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error running go doc: %v\nOutput: %s", err, string(out))}
	}
	return tools.ToolResult{Text: string(out)}
}

func (m *infoManager) GetFileSkeleton(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Filepath string `json:"filepath"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path, err := m.SP.IsPathSafe(params.Filepath)
	if err != nil {
		return tools.ToolResult{}, err
	}

	var res tools.ToolResult
	if filepath.Ext(path) == ".go" {
		// path is absolute from IsPathSafe. absPath inside the cache
		// detects absolute paths and passes them through unchanged.
		skeleton, err := m.Cache.GetFileSkeletonGo(path)
		if err == nil {
			res = tools.ToolResult{Text: skeleton}
		} else {
			// Fallback to generic if AST parsing fails
			res, err = m.extractGenericSkeleton(ctx, path)
			if err != nil {
				return tools.ToolResult{}, err
			}
		}
	} else {
		res, err = m.extractGenericSkeleton(ctx, path)
		if err != nil {
			return tools.ToolResult{}, err
		}
	}

	res.Text += "\n\n// SYSTEM HINT: To view the full function bodies, call 'read_files' on this filepath."
	return res, nil
}

func (m *infoManager) extractGenericSkeleton(ctx context.Context, path string) (tools.ToolResult, error) {
	content, err := m.FS.ReadFile(ctx, path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	var sb strings.Builder
	lines := strings.Split(string(content), "\n")
	var lastComments []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if m.isCommentOrEmpty(trimmed) {
			if trimmed == "" {
				lastComments = nil
			} else {
				lastComments = append(lastComments, line)
			}
			continue
		}

		if m.isDefinitionLine(line) {
			m.writeDefinitionWithComments(&sb, line, lastComments)
		}
		lastComments = nil
	}

	res := strings.TrimSpace(sb.String())
	if res == "" {
		return tools.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}
	return tools.ToolResult{Text: res}, nil
}

func (m *infoManager) isCommentOrEmpty(trimmed string) bool {
	return trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#")
}

func (m *infoManager) writeDefinitionWithComments(sb *strings.Builder, line string, lastComments []string) {
	for _, c := range lastComments {
		sb.WriteString(c + "\n")
	}
	sb.WriteString(line + "\n\n")
}

func (m *infoManager) isDefinitionLine(line string) bool {
	for _, p := range genericSkeletonPatterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
}

func (m *infoManager) countLines(ctx context.Context, path string) (int, error) {
	f, err := m.FS.Open(ctx, path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
