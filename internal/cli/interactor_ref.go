// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"sync/atomic"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// InteractorRef is a thread-safe, mutable cell that holds the active
// UserInteractor. It exists to break the bootstrap-ordering cycle between
// SecurityManager (constructed early) and Capturer (constructed lazily, after
// the CLI command starts). Reads and writes are safe across goroutines.
//
// Wiring contract:
//   - The composition root (cmd/tell-me-go/main.go) creates one InteractorRef
//     and passes it to both the SecurityManager (via a provider closure that
//     calls Get) and to the CLI App (via AppDependencies.Interactor).
//   - The CLI command (chat or browse) calls Set(capturer) once the
//     UserInteractor-capable Capturer has been constructed.
//   - SecurityManager's interaction handler invokes the provider on each
//     interaction, observing the latest Set value atomically.
type InteractorRef struct {
	p atomic.Pointer[domain_security.UserInteractor]
}

// NewInteractorRef returns an empty, ready-to-use InteractorRef. Get returns
// nil until Set is called.
func NewInteractorRef() *InteractorRef {
	return &InteractorRef{}
}

// Set atomically stores the given interactor. Safe to call from any goroutine.
// Passing a nil interactor effectively clears the cell.
func (r *InteractorRef) Set(i domain_security.UserInteractor) {
	if r == nil {
		return
	}
	r.p.Store(&i)
}

// Get atomically loads the current interactor. Returns nil if Set has not
// been called yet (callers should fall back to a no-op interactor).
func (r *InteractorRef) Get() domain_security.UserInteractor {
	if r == nil {
		return nil
	}
	if p := r.p.Load(); p != nil {
		return *p
	}
	return nil
}
