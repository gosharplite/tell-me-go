// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"sync"
	"testing"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// Compile-time assertion: stubInteractor must satisfy UserInteractor.
// This keeps the contract explicit and fails the build if the interface
// drifts (e.g., a method is added to UserInteractor).
var _ domain_security.UserInteractor = (*stubInteractor)(nil)

// stubInteractor is a minimal UserInteractor used to verify atomic storage
// in InteractorRef. It carries an identifier so tests can assert which
// instance was loaded.
type stubInteractor struct{ id int }

func (s *stubInteractor) Confirm(_ stdctx.Context, _ string) (bool, error) { return false, nil }
func (s *stubInteractor) Warn(_ string)                                    {}
func (s *stubInteractor) Prompt(_ string)                                  {}
func (s *stubInteractor) ReadSingleKey(_ stdctx.Context) (string, error)   { return "", nil }
func (s *stubInteractor) ReadLine(_ stdctx.Context) (string, error)        { return "", nil }

func TestInteractorRef_GetBeforeSetReturnsNil(t *testing.T) {
	t.Parallel()
	ref := NewInteractorRef()
	if got := ref.Get(); got != nil {
		t.Fatalf("expected nil before Set, got %T", got)
	}
}

func TestInteractorRef_SetThenGet(t *testing.T) {
	t.Parallel()
	ref := NewInteractorRef()
	want := &stubInteractor{id: 42}
	ref.Set(want)

	got := ref.Get()
	if got == nil {
		t.Fatal("expected non-nil interactor after Set")
	}
	stub, ok := got.(*stubInteractor)
	if !ok {
		t.Fatalf("expected *stubInteractor, got %T", got)
	}
	if stub.id != 42 {
		t.Errorf("expected id=42, got id=%d", stub.id)
	}
}

func TestInteractorRef_OverwriteWins(t *testing.T) {
	t.Parallel()
	ref := NewInteractorRef()
	ref.Set(&stubInteractor{id: 1})
	ref.Set(&stubInteractor{id: 2})

	stub, ok := ref.Get().(*stubInteractor)
	if !ok {
		t.Fatalf("expected *stubInteractor, got %T", ref.Get())
	}
	if stub.id != 2 {
		t.Errorf("expected latest Set to win (id=2), got id=%d", stub.id)
	}
}

func TestInteractorRef_NilReceiverIsSafe(t *testing.T) {
	t.Parallel()
	var ref *InteractorRef // intentionally nil

	// Must not panic.
	ref.Set(&stubInteractor{id: 99})
	if got := ref.Get(); got != nil {
		t.Errorf("expected nil from nil receiver, got %T", got)
	}
}

// TestInteractorRef_ConcurrentAccess exercises the atomic semantics under
// the race detector. Without the InteractorRef refactor, this is a plain
// interface-value read/write race that `go test -race` would flag.
func TestInteractorRef_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	ref := NewInteractorRef()
	ref.Set(&stubInteractor{id: 0})

	const writers, readers, iters = 4, 8, 1000

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				ref.Set(&stubInteractor{id: w*iters + i})
			}
		}()
	}

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				// Read must never return nil (a writer always set first)
				// and must always satisfy the interface.
				if v := ref.Get(); v == nil {
					t.Error("unexpected nil read from concurrently written ref")
					return
				}
			}
		}()
	}

	wg.Wait()
}
