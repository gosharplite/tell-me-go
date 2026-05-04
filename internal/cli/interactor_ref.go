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
//   - The CLI command (chat or browse) calls set(capturer) — unexported,
//     internal to package cli — once the Capturer has been constructed.
//   - SecurityManager's interaction handler invokes the provider on each
//     interaction, observing the latest set value atomically.
//
// Note on API surface: Get is exported so the bootstrap (cmd/tell-me-go)
// can read the cell from its provider closure. set is intentionally
// unexported because mutation is a CLI-internal lifecycle concern; allowing
// external packages to mutate the cell would re-introduce the temporal-
// coupling smell that issue #131 was designed to eliminate.
type InteractorRef struct {
	p atomic.Pointer[domain_security.UserInteractor]
}

// NewInteractorRef returns an empty, ready-to-use InteractorRef. Get returns
// nil until set is called.
func NewInteractorRef() *InteractorRef {
	return &InteractorRef{}
}

// set atomically stores the given interactor. Safe to call from any goroutine.
// Passing a nil interactor effectively clears the cell. Unexported by design
// (see the type-level note on API surface).
func (r *InteractorRef) set(i domain_security.UserInteractor) {
	if r == nil {
		return
	}
	r.p.Store(&i)
}

// Get atomically loads the current interactor. Returns nil if set has not
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
