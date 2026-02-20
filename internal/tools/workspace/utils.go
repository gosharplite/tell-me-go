// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

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

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// fileProcessor is a callback function for processing a file during a walk.
type fileProcessor func(filePath string) error

// walkAndProcess handles the generic filesystem traversal, safety checks, and directory filtering.
func walkAndProcess(ctx context.Context, sm domain_security.PathValidator, fs persistence.FileSystem, path string, fn fileProcessor) error {
	if path == "" {
		path = "."
	}
	var err error
	path, err = sm.IsPathSafe(path)
	if err != nil {
		return err
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
func ConcurrentSearch(ctx context.Context, sp domain_security.PathValidator, fs persistence.FileSystem, root string, matcher func(path, line string) bool, limit int) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if limit <= 0 {
		limit = 100
	}

	// Safety check
	resolvedRoot, err := sp.IsPathSafe(root)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := &searchPipeline{
		fs:          fs,
		matcher:     matcher,
		limit:       limit,
		pathsChan:   make(chan string, 100),
		resultsChan: make(chan string, 100),
		errChan:     make(chan error, 1),
		root:        resolvedRoot,
		ctx:         ctx,
		cancel:      cancel,
	}

	return p.Execute()
}

type searchPipeline struct {
	fs          persistence.FileSystem
	matcher     func(path, line string) bool
	limit       int
	pathsChan   chan string
	resultsChan chan string
	errChan     chan error
	root        string
	ctx         context.Context
	cancel      context.CancelFunc
}

func (p *searchPipeline) Execute() ([]string, error) {
	p.startWalker()

	var wg sync.WaitGroup
	p.startWorkers(&wg)

	go func() {
		wg.Wait()
		close(p.resultsChan)
	}()

	return p.collectResults()
}

func (p *searchPipeline) startWalker() {
	go func() {
		defer close(p.pathsChan)
		err := p.fs.Walk(p.ctx, p.root, p.walkFunc)
		if err != nil && err != context.Canceled {
			select {
			case p.errChan <- err:
			default:
			}
		}
	}()
}

func (p *searchPipeline) walkFunc(path string, info os.FileInfo, err error) error {
	if err != nil {
		return nil // Skip
	}
	if p.ctx.Err() != nil {
		return p.ctx.Err()
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
	case p.pathsChan <- path:
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
	return nil
}

func (p *searchPipeline) startWorkers(wg *sync.WaitGroup) {
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range p.pathsChan {
				if err := p.scanFile(path); err != nil {
					if err == context.Canceled || err == context.DeadlineExceeded {
						return
					}
				}
			}
		}()
	}
}

func (p *searchPipeline) scanFile(path string) error {
	file, err := p.fs.Open(p.ctx, path)
	if err != nil {
		return nil
	}
	defer file.Close()

	if isBin, err := checkBinary(file); err == nil && !isBin {
		const maxScannerCapacity = 10 * 1024 * 1024
		scanner := bufio.NewScanner(file)
		// Replace sync.Pool with a simple local allocation.
		// Go's GC handles short-lived 64KB buffers incredibly fast, and this avoids the pointer-growth leak.
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, maxScannerCapacity)

		lineNum := 0
		for scanner.Scan() {
			// Check for cancellation to prevent unbounded CPU burn on large files
			if p.ctx.Err() != nil {
				return p.ctx.Err()
			}

			lineNum++
			line := scanner.Text()
			if p.matcher(path, line) {
				select {
				case p.resultsChan <- formatMatch(path, lineNum, line):
				case <-p.ctx.Done():
					return p.ctx.Err()
				}
			}
		}
		return scanner.Err()
	}
	return nil
}

func (p *searchPipeline) collectResults() ([]string, error) {
	var results []string
	var finalErr error
	for {
		select {
		case res, ok := <-p.resultsChan:
			if !ok {
				return results, finalErr
			}
			results, finalErr = p.handleResult(res, results, finalErr)
		case err := <-p.errChan:
			finalErr = err
			p.cancel()
		case <-p.ctx.Done():
			return p.handleDone(results, finalErr)
		}
	}
}

func (p *searchPipeline) handleResult(res string, results []string, finalErr error) ([]string, error) {
	if len(results) < p.limit {
		results = append(results, res)
	}
	if len(results) >= p.limit && finalErr == nil {
		p.cancel()
		finalErr = fmt.Errorf("too many results")
	}
	return results, finalErr
}

func (p *searchPipeline) handleDone(results []string, finalErr error) ([]string, error) {
	if finalErr == nil {
		finalErr = p.ctx.Err()
	}
	// Drain remaining results if any
	for res := range p.resultsChan {
		if len(results) < p.limit {
			results = append(results, res)
		}
	}
	return results, finalErr
}

// checkBinary reads the beginning of the file to check for binary content and rewinds the cursor.
func checkBinary(file persistence.File) (bool, error) {
	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return false, err
	}
	return persistence.IsBinary(buf[:n]), nil
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
