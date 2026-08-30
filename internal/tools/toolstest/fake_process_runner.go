// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// FakeProcessRunner is a test double for tools.ProcessRunner. It follows the
// FakeToolchainRunner shape: pre-set return values via <Method>Func fields
// plus a call log recording every invocation as the method name, in call
// order. Start records to the log before invoking StartFunc (outside the
// lock, per the non-reentrant deadlock rule); an unset StartFunc returns
// (nil, nil). This is the canonical tools-family runner double per ADR-021
// locality and ADR-056 canonical-mock-home discipline (issue #1460,
// ADR-074).
type FakeProcessRunner struct {
	StartFunc func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error)

	// mu guards Calls for concurrent access: workspace tools may invoke the
	// runner concurrently, so the call log must be race-safe (issue #1460).
	mu    sync.Mutex
	Calls []string
}

// Called reports whether the named method was invoked at least once.
func (f *FakeProcessRunner) Called(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		if c == method {
			return true
		}
	}
	return false
}

func (f *FakeProcessRunner) Start(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "Start")
	f.mu.Unlock()
	if f.StartFunc != nil {
		return f.StartFunc(ctx, spec)
	}
	return nil, nil
}
