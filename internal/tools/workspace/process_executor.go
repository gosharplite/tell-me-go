// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/encoding"
	"github.com/gosharplite/tell-me-go/internal/pkg/filepathutil"
)

// osGetwd is a test hook for os.Getwd. It defaults to os.Getwd and
// is overridden in tests to exercise error paths in validateAbsPath
// and validateAndResolveRelPath.
var osGetwd = os.Getwd

// executionConfig defines parameters for command or pipeline execution.
type executionConfig struct {
	OutputFile string
	Append     bool
	MaxCapture int
	Feedback   io.Writer
	Env        map[string]string
}

// executionResult holds the outcome of an execution.
type executionResult struct {
	Output    string
	Error     string
	ExitCode  int
	Truncated bool
}

// processExecutor handles running external commands and pipelines. The
// os/exec process lifecycle lives behind the injected tools.ProcessRunner
// port (issue #1460, ADR-074); this type owns command assembly, output
// capture, and result interpretation.
type processExecutor struct {
	fs     persistence.FileSystem
	runner tools.ProcessRunner
}

const maxScannerCapacity = 10 * 1024 * 1024

// newprocessExecutorWithFS creates a processExecutor with an explicit
// filesystem and process runner. A nil fs or nil runner is a contract
// violation on a test-reachable seam: in production the hub
// (validateRegistrationParams) guarantees non-nil for both.
func newprocessExecutorWithFS(fs persistence.FileSystem, runner tools.ProcessRunner) *processExecutor {
	if fs == nil || runner == nil {
		panic("newprocessExecutorWithFS: nil fs/nil runner — inject the filesystem and process runner via ToolRegistrationParams (ADR-074)")
	}
	return &processExecutor{fs: fs, runner: runner}
}

// RunCommand executes a single command.
func (e *processExecutor) RunCommand(ctx context.Context, parts []string, config executionConfig) (res executionResult, err error) {
	// Coverage gap accepted by architect — len(parts)==0 is a defensive
	// guard on already-validated input; callers always provide non-empty
	// slices constructed from parsed shell arguments.
	if len(parts) == 0 {
		return executionResult{ExitCode: 1}, fmt.Errorf("empty command")
	}

	file := e.prepareOutputFile(ctx, config)
	if file != nil {
		defer closeFile(file, &err)
	}

	// architect-acceptance: subprocess invocation fault injection — see the fault-injection-required acceptance class (INTENTIONAL_NON_FIXES.md)
	h, serr := e.runner.Start(ctx, tools.ProcessSpec{Name: parts[0], Args: parts[1:], Stdin: nil, Env: config.Env})
	if serr != nil {
		return executionResult{ExitCode: 1}, fmt.Errorf("failed to start: %w", serr)
	}

	var sb strings.Builder
	truncated := e.captureOutput(&sb, h.Stdout(), h.Stderr(), config, file)

	waitErr := h.Wait()
	if ctx.Err() != nil {
		return executionResult{Output: sb.String(), ExitCode: 1}, ctx.Err()
	}

	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		var exitErr *tools.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.Code
		} else {
			return executionResult{Output: sb.String(), ExitCode: exitCode}, waitErr
		}
	}

	return executionResult{
		Output:    sb.String(),
		ExitCode:  exitCode,
		Truncated: truncated.Load(),
	}, nil
}

func (e *processExecutor) prepareOutputFile(ctx context.Context, config executionConfig) *os.File {
	file, ferr := e.openOutputFile(ctx, config)
	if ferr != nil && config.Feedback != nil {
		_, _ = fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file %q: %v\n", config.OutputFile, ferr)
	}
	return file
}

func (e *processExecutor) captureOutput(sb *strings.Builder, stdout, stderr io.Reader, config executionConfig, file *os.File) *atomic.Bool {
	var mu sync.Mutex
	var wg sync.WaitGroup
	truncated := &atomic.Bool{}
	wt := &writeTracker{feedback: config.Feedback, filePath: config.OutputFile}
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	wg.Add(2)
	totalCaptured := 0
	go e.captureStream(encoding.WrapReader(stdout), false, sb, &mu, &wg, truncated, wt, config, file, maxCapture, &totalCaptured)
	go e.captureStream(encoding.WrapReader(stderr), true, sb, &mu, &wg, truncated, wt, config, file, maxCapture, &totalCaptured)
	wg.Wait()

	return truncated
}

func (e *processExecutor) captureStream(r io.Reader, isStderr bool, sb *strings.Builder, mu *sync.Mutex, wg *sync.WaitGroup, truncated *atomic.Bool, wt *writeTracker, config executionConfig, file *os.File, maxCapture int, totalCaptured *int) {
	defer wg.Done()

	sp := &streamProcessor{
		mu:            mu,
		truncated:     truncated,
		totalCaptured: totalCaptured,
		wt:            wt,
		maxCapture:    maxCapture,
		feedback:      config.Feedback,
		file:          file,
	}

	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)

	prefix := ""
	if isStderr {
		prefix = "[stderr]"
	}

	for scanner.Scan() {
		sp.processLine(sb, scanner.Bytes(), prefix, config.Feedback)
	}

	e.handleCaptureError(scanner.Err(), sb, mu, config, truncated, maxCapture)
}

func (e *processExecutor) handleCaptureError(err error, sb *strings.Builder, mu *sync.Mutex, config executionConfig, truncated *atomic.Bool, maxCapture int) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
	if err == bufio.ErrTooLong {
		msg = "\n[Warning] Output line too long for scanner; truncated."
	}

	mu.Lock()
	defer mu.Unlock()
	if config.Feedback != nil {
		_, _ = fmt.Fprintln(config.Feedback, msg)
	}

	remaining := maxCapture - sb.Len()
	if remaining > 0 {
		fullMsg := msg + "\n"
		if len(fullMsg) > remaining {
			truncated.Store(true)
		}
		content := sanitizeAndTruncateUTF8(fullMsg, remaining)
		sb.WriteString(content)
	} else {
		truncated.Store(true)
	}
}

// RunPipeline executes a sequence of piped commands.
func (e *processExecutor) RunPipeline(ctx context.Context, pipedParts [][]string, config executionConfig) (res executionResult, err error) {
	if len(pipedParts) < 2 {
		return executionResult{ExitCode: 1}, fmt.Errorf("at least two commands are required for piping")
	}

	p, setupErr := e.newPipeline(ctx, pipedParts, config)
	if setupErr != nil {
		return executionResult{ExitCode: 1}, setupErr
	}
	defer p.closeHandles()

	file := e.prepareOutputFile(ctx, config)
	if file != nil {
		defer closeFile(file, &err)
	}

	if err = p.start(ctx); err != nil {
		p.closeHandles() // Close handed-out readers to unblock running commands
		_, _ = p.wait()  // Ensure started processes are cleaned up
		return executionResult{ExitCode: 1}, fmt.Errorf("pipeline failed to start: %w", err)
	}

	stdoutStr, stderrStr, truncated := p.capture(config, file)
	exitCode, waitErr := p.wait()

	if ctx.Err() != nil {
		return e.formatPipelineResult(stdoutStr, stderrStr, truncated, 1, ctx.Err())
	}

	return e.formatPipelineResult(stdoutStr, stderrStr, truncated, exitCode, waitErr)
}

func (e *processExecutor) formatPipelineResult(stdoutStr, stderrStr string, truncated bool, exitCode int, waitErr error) (executionResult, error) {
	output := stdoutStr
	if stderrStr != "" {
		output = fmt.Sprintf("Output:\n%s\nErrors:\n%s", stdoutStr, stderrStr)
	}

	// Ensure exit code is non-zero if waitErr occurred
	if waitErr != nil && exitCode == 0 {
		exitCode = 1
	}

	if waitErr != nil {
		var exitErr *tools.ExitError
		if !errors.As(waitErr, &exitErr) {
			return executionResult{
				Output:    output,
				ExitCode:  exitCode,
				Truncated: truncated,
			}, waitErr
		}
	}

	return executionResult{
		Output:    output,
		ExitCode:  exitCode,
		Truncated: truncated,
	}, nil
}

// withinParent reports whether target resides inside the parent directory.
func withinParent(parent, target string) bool {
	// Resolve both paths to a canonical form for consistent cross-platform
	// comparison. On Windows, os.Getwd() and filepath.Abs may return paths
	// with different normalization (short vs long names, case differences)
	// causing filepath.Rel to fail even for valid parent-child relationships.
	parent = filepathutil.NormalizePath(parent)
	target = filepathutil.NormalizePath(target)

	rel, err := filepath.Rel(parent, target)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	return true
}

// validateAbsPath checks an absolute cleanedPath against CWD and TempDir boundaries.
// originalPath is used only for error messages.
func validateAbsPath(cleanedPath, originalPath string) (string, error) {
	// 1. Check CWD boundary.
	cwd, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	if withinParent(cwd, cleanedPath) {
		return cleanedPath, nil
	}

	// 2. Check os.TempDir() boundary (skip if empty).
	if tmpDir := os.TempDir(); tmpDir != "" {
		if withinParent(tmpDir, cleanedPath) {
			return cleanedPath, nil
		}
	}

	return "", fmt.Errorf("output file path cannot escape current directory: %q", originalPath)
}

// validateAndResolveRelPath resolves a relative cleanedPath against CWD
// and checks that it does not escape. originalPath is used only for error messages.
func validateAndResolveRelPath(cleanedPath, originalPath string) (string, error) {
	cwd, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	absPath := filepath.Join(cwd, cleanedPath)

	if !withinParent(cwd, absPath) {
		return "", fmt.Errorf("output file path cannot escape current directory: %q", originalPath)
	}
	return absPath, nil
}

// resolveAndValidateOutputPath converts a cleaned path to an absolute path,
// validating that it does not escape the current working directory or the
// system temporary directory. originalPath is used only for the error message.
func resolveAndValidateOutputPath(cleanedPath, originalPath string) (string, error) {
	if filepath.IsAbs(cleanedPath) {
		return validateAbsPath(cleanedPath, originalPath)
	}

	// Coverage gap accepted by architect — the Windows separator-leading
	// path branch is platform-specific and only executable on Windows.
	// Same acceptance class as other platform-specific branches.
	if runtime.GOOS == "windows" && len(cleanedPath) > 0 && os.IsPathSeparator(cleanedPath[0]) {
		cwd, err := osGetwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		vol := filepath.VolumeName(cwd)
		if vol != "" {
			return validateAbsPath(vol+cleanedPath, originalPath)
		}
		// No volume (UNC path edge case) — reject
		return "", fmt.Errorf("output file path cannot escape current directory: %q", originalPath)
	}

	return validateAndResolveRelPath(cleanedPath, originalPath)
}

func (e *processExecutor) openOutputFile(ctx context.Context, config executionConfig) (*os.File, error) {
	if config.OutputFile == "" {
		return nil, nil
	}

	// CRITICAL: Strip null bytes BEFORE any other path processing to avoid Windows issues
	path := strings.ReplaceAll(config.OutputFile, "\x00", "")
	path = strings.TrimSpace(path)

	if path == "" {
		return nil, nil
	}
	path = filepath.Clean(path)

	var resolveErr error
	path, resolveErr = resolveAndValidateOutputPath(path, config.OutputFile)
	if resolveErr != nil {
		return nil, resolveErr
	}

	flags := os.O_CREATE | os.O_WRONLY
	if config.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	if err := e.fs.MkdirAll(ctx, filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return os.OpenFile(path, flags, 0644)
}

// writeTracker tracks if a write to a shared output file has failed,
// ensuring only one warning is issued.
type writeTracker struct {
	failed   atomic.Bool
	feedback io.Writer
	filePath string
}

// Write attempts to write to w. If it fails, it sets the failed flag and
// optionally sends a warning to feedback.
func (wt *writeTracker) Write(w io.Writer, p []byte) {
	if wt.failed.Load() || w == nil {
		return
	}

	// Robustness check for typed nils (e.g., *os.File(nil) passed as io.Writer)
	if f, ok := w.(*os.File); ok && f == nil {
		return
	}

	if _, err := w.Write(p); err != nil {
		if wt.failed.CompareAndSwap(false, true) {
			if wt.feedback != nil {
				_, _ = fmt.Fprintf(wt.feedback, "\n[Warning] Failed to write to output file %q: %v\n", wt.filePath, err)
			}
		}
	}
}

// sanitizeAndTruncateUTF8 strips invalid UTF-8 sequences from s, then
// ensures the result does not exceed maxBytes without breaking a
// multi-byte UTF-8 character at the boundary.
func sanitizeAndTruncateUTF8(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// closeFile closes a closer and promotes any close error to *err when
// *err is nil. This prevents close failures from being silently swallowed
// when the primary operation succeeded.
func closeFile(closer io.Closer, err *error) {
	if cerr := closer.Close(); cerr != nil && *err == nil {
		*err = fmt.Errorf("failed to close output file: %w", cerr)
	}
}

// Output runs the command and returns its standard output.
func (e *processExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := e.RunCommand(ctx, append([]string{name}, args...), executionConfig{})
	if err != nil {
		return []byte(res.Output), err
	}
	if res.ExitCode != 0 {
		return []byte(res.Output), fmt.Errorf("exit status %d", res.ExitCode)
	}
	return []byte(res.Output), nil
}

// CombinedOutput runs the command and returns its combined standard output and standard error.
func (e *processExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := e.RunCommand(ctx, append([]string{name}, args...), executionConfig{})
	if err != nil {
		return []byte(res.Output), err
	}
	if res.ExitCode != 0 {
		return []byte(res.Output), fmt.Errorf("exit status %d", res.ExitCode)
	}
	return []byte(res.Output), nil
}

// architect-acceptance: delegation wrapper — see the delegation-wrapper acceptance class (INTENTIONAL_NON_FIXES.md)
func (e *processExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}
