// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
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

// walkHeartbeat emits a heartbeat every 50 files walked, respecting context cancellation.
// Returns an error if the context is done.
func walkHeartbeat(ctx context.Context, count int, hb chan<- struct{}) error {
	if count%50 == 0 && hb != nil {
		sendHeartbeat(ctx, hb)
		return ctx.Err()
	}
	return nil
}

// shouldSkipEntry checks whether a walk entry should be skipped (inaccessible, cancelled, or directory).
func shouldSkipEntry(ctx context.Context, info os.FileInfo, err error, policy services.WorkspacePolicy) (bool, error) {
	if err != nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if info.IsDir() {
		if policy.ShouldIgnoreDir(info.Name()) {
			return true, filepath.SkipDir
		}
		return true, nil
	}
	return false, nil
}

// walkAndProcess handles the generic filesystem traversal, safety checks, and directory filtering.
func walkAndProcess(ctx context.Context, sm domain_security.PathValidator, fs persistence.FileSystem, path string, hb chan<- struct{}, fn fileProcessor, policy services.WorkspacePolicy) error {
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
		if skip, retErr := shouldSkipEntry(ctx, info, err, policy); skip {
			return retErr
		}

		count++
		if err := walkHeartbeat(ctx, count, hb); err != nil {
			return err
		}

		return fn(filePath)
	})
}
