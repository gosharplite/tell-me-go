// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/system"
)

type mockFileSystem struct {
	fsutil.FileSystem
	files map[string][]byte
}

func (m *mockFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if data, ok := m.files[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) Walk(ctx context.Context, root string, fn fsutil.WalkFunc) error {
	for path, data := range m.files {
		// In mock, we ignore root and visit all registered files
		info := &mockFileInfo{name: path, size: int64(len(data))}
		if err := fn(path, info, nil); err != nil {
			return err
		}
	}
	return nil
}

type mockFileInfo struct {
	os.FileInfo
	name string
	size int64
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

type mockCommandExecutor struct {
	system.CommandExecutor
	runFunc func(ctx context.Context, parts []string, config system.ExecutionConfig) (system.ExecutionResult, error)
}

func (m *mockCommandExecutor) RunCommand(ctx context.Context, parts []string, config system.ExecutionConfig) (system.ExecutionResult, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, parts, config)
	}
	return system.ExecutionResult{ExitCode: 0}, nil
}

func TestVerifyReleaseReadiness_Success(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	
	fs := &mockFileSystem{
		files: map[string][]byte{
			"go.mod": []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"),
			"main.go": []byte("package main\nfunc main() {}"),
		},
	}
	
	executor := &mockCommandExecutor{}
	
	m := &releaseManager{
		sm:       sm,
		fs:       fs,
		executor: executor,
	}
	
	res, err := m.verifyReleaseReadiness(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if !strings.Contains(res.Text, "READY FOR RELEASE") {
		t.Errorf("expected READY FOR RELEASE, got:\n%s", res.Text)
	}
}

func TestVerifyReleaseReadiness_Failures(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")

	tests := []struct {
		name       string
		files      map[string][]byte
		exitCode   int
		wantSubstr string
	}{
		{
			name: "Secret found",
			files: map[string][]byte{
				"go.mod": []byte("module test"),
				"config.go": []byte("apiKey := \"private_key\""),
			},
			wantSubstr: "Potential secret",
		},
		{
			name: "Replace directive",
			files: map[string][]byte{
				"go.mod": []byte("module test\nreplace foo => ../foo"),
			},
			wantSubstr: "contains 'replace' directives",
		},
		{
			name: "Build failure",
			files: map[string][]byte{
				"go.mod": []byte("module test"),
			},
			exitCode:   1,
			wantSubstr: "Clean build failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &mockFileSystem{files: tt.files}
			executor := &mockCommandExecutor{
				runFunc: func(ctx context.Context, parts []string, config system.ExecutionConfig) (system.ExecutionResult, error) {
					// Build and Test are the two commands executed
					return system.ExecutionResult{ExitCode: tt.exitCode, Output: "failed"}, nil
				},
			}
			
			m := &releaseManager{
				sm:       sm,
				fs:       fs,
				executor: executor,
			}
			
			res, err := m.verifyReleaseReadiness(context.Background(), nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			
			if !strings.Contains(res.Text, tt.wantSubstr) {
				t.Errorf("expected substring %q in report, got:\n%s", tt.wantSubstr, res.Text)
			}
			if !strings.Contains(res.Text, "NOT READY") {
				t.Errorf("expected NOT READY in report, got:\n%s", res.Text)
			}
		})
	}
}
