// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

// fileProcessor is a callback function for processing a file during a walk.
type fileProcessor func(filePath string) error

// walkAndProcess handles the generic filesystem traversal, safety checks, and directory filtering.
func walkAndProcess(ctx context.Context, sm *security.SecurityManager, fs fsutil.FileSystem, path string, fn fileProcessor) error {
	// If path isn't absolute/resolved yet, check safety
	if !filepath.IsAbs(path) {
		if path == "" {
			path = "."
		}
		var err error
		path, err = sm.IsPathSafe(path)
		if err != nil {
			return err
		}
	}

	return fs.Walk(ctx, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip items we can't access
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if info.IsDir() {
			if isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		return fn(filePath)
	})
}

// scanFile opens a file, checks for binary content, and scans lines with a matcher function.
func scanFile(ctx context.Context, fs fsutil.FileSystem, filePath string, matcher func(string) bool, results *[]string) error {
	file, err := fs.Open(ctx, filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	if isBin, err := checkBinary(file); err != nil || isBin {
		return nil
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if matcher(line) {
			*results = append(*results, formatMatch(filePath, lineNum, line))
			if len(*results) > 100 {
				return fmt.Errorf("too many results")
			}
		}
	}
	return nil
}

// checkBinary reads the beginning of the file to check for binary content and rewinds the cursor.
func checkBinary(file fsutil.File) (bool, error) {
	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return false, err
	}
	return fsutil.IsBinary(buf[:n]), nil
}

func isIgnoredDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == "vendor"
}

func formatMatch(path string, lineNum int, text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 500 {
		trimmed = trimmed[:500] + " (truncated)"
	}
	return fmt.Sprintf("%s:%d: %s", path, lineNum, trimmed)
}
