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

// sendHeartbeat safely sends a heartbeat, ignoring panics if the channel is closed.
func sendHeartbeat(ctx context.Context, hb chan<- struct{}) {
	if hb == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	select {
	case hb <- struct{}{}:
	case <-ctx.Done():
	default:
	}
}

// walkAndProcess handles the generic filesystem traversal, safety checks, and directory filtering.
func walkAndProcess(ctx context.Context, sm domain_security.PathValidator, fs persistence.FileSystem, path string, hb chan<- struct{}, fn fileProcessor) error {
	if path == "" {
		path = "."
	}
	var err error
	path, err = sm.IsPathSafe(path)
	if err != nil {
		return err
	}

	count := 0
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

		count++
		if count%50 == 0 && hb != nil {
			sendHeartbeat(ctx, hb)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		return fn(filePath)
	})
}

// ConcurrentSearch walks the path and processes files in parallel using workers.
func ConcurrentSearch(ctx context.Context, sp domain_security.PathValidator, fs persistence.FileSystem, root string, hb chan<- struct{}, matcher func(path, line string) (string, bool)) (<-chan string, <-chan error) {
	errChan := make(chan error, 1)
	if ctx.Err() != nil {
		errChan <- ctx.Err()
		close(errChan)
		resChan := make(chan string)
		close(resChan)
		return resChan, errChan
	}

	// Safety check
	resolvedRoot, err := sp.IsPathSafe(root)
	if err != nil {
		errChan <- err
		close(errChan)
		resChan := make(chan string)
		close(resChan)
		return resChan, errChan
	}

	p := &searchPipeline{
		fs:          fs,
		matcher:     matcher,
		pathsChan:   make(chan string, 100),
		resultsChan: make(chan string, 100),
		errChan:     errChan,
		hb:          hb,
		root:        resolvedRoot,
		ctx:         ctx,
	}

	return p.Execute()
}

type searchPipeline struct {
	fs          persistence.FileSystem
	matcher     func(path, line string) (string, bool)
	pathsChan   chan string
	resultsChan chan string
	errChan     chan error
	hb          chan<- struct{}
	root        string
	ctx         context.Context
}

func (p *searchPipeline) Execute() (<-chan string, <-chan error) {
	p.startWalker()

	var wg sync.WaitGroup
	p.startWorkers(&wg)

	go func() {
		wg.Wait()
		close(p.resultsChan)
	}()

	return p.resultsChan, p.errChan
}

func (p *searchPipeline) startWalker() {
	go func() {
		defer close(p.pathsChan)
		defer close(p.errChan)
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

	// Heartbeat while walking
	if p.hb != nil {
		sendHeartbeat(p.ctx, p.hb)
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
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
	defer func() { _ = file.Close() }()

	// Heartbeat before scanning a file
	if p.hb != nil {
		sendHeartbeat(p.ctx, p.hb)
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
	}

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
			if match, ok := p.matcher(path, line); ok {
				text := line
				if match != "" {
					text = match
				}
				select {
				case p.resultsChan <- formatMatch(path, lineNum, text):
				case <-p.ctx.Done():
					return p.ctx.Err()
				}
			}
		}
		return scanner.Err()
	}
	return nil
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
