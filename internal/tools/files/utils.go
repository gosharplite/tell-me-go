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
	"runtime"
	"strings"
	"sync"

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

// ConcurrentSearch walks the path and processes files in parallel using workers.
func ConcurrentSearch(ctx context.Context, sp security.SecurityProvider, fs fsutil.FileSystem, root string, matcher func(path, line string) bool, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}

	// Safety check
	resolvedRoot, err := sp.IsPathSafe(root)
	if err != nil {
		return nil, err
	}

	paths := make(chan string, 100)
	resultsChan := make(chan string, 100)
	errChan := make(chan error, 1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 1. Walking Goroutine
	go func() {
		defer close(paths)
		err := fs.Walk(ctx, resolvedRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip
			}
			if info.IsDir() {
				if isIgnoredDir(info.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			// Skip files > 1MB
			if info.Size() > 1024*1024 {
				return nil
			}
			select {
			case paths <- path:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		})
		if err != nil && err != context.Canceled {
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	// 2. Workers
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range paths {
				file, err := fs.Open(ctx, path)
				if err != nil {
					continue
				}

				if isBin, err := checkBinary(file); err == nil && !isBin {
					scanner := bufio.NewScanner(file)
					lineNum := 0
					for scanner.Scan() {
						lineNum++
						line := scanner.Text()
						if matcher(path, line) {
							select {
							case resultsChan <- formatMatch(path, lineNum, line):
							case <-ctx.Done():
								file.Close()
								return
							}
						}
					}
				}
				file.Close()
			}
		}()
	}

	// 3. Collector
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var results []string
	var finalErr error
	for {
		select {
		case res, ok := <-resultsChan:
			if !ok {
				return results, finalErr
			}
			if len(results) < limit {
				results = append(results, res)
			}
			if len(results) >= limit && finalErr == nil {
				cancel()
				finalErr = fmt.Errorf("too many results")
			}
		case err := <-errChan:
			finalErr = err
			cancel()
		case <-ctx.Done():
			if finalErr == nil {
				finalErr = ctx.Err()
			}
			// Drain remaining results if any
			for res := range resultsChan {
				if len(results) < limit {
					results = append(results, res)
				}
			}
			return results, finalErr
		}
	}
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
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error scanning file %s: %w", filePath, err)
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
	return name == ".git" || name == "node_modules" || name == "vendor" || name == "output" || name == "dist"
}

func formatMatch(path string, lineNum int, text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 500 {
		trimmed = trimmed[:500] + " (truncated)"
	}
	return fmt.Sprintf("%s:%d: %s", path, lineNum, trimmed)
}
