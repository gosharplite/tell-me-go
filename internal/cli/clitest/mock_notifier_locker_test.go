// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

func TestMockCallbackNotifier(t *testing.T) {
	t.Run("default nil NotifyFunc returns nil", func(t *testing.T) {
		m := &clitest.MockCallbackNotifier{}
		err := m.Notify(context.Background(), "http://localhost", nil, domain_callback.CallbackPayload{})
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("delegates to NotifyFunc", func(t *testing.T) {
		customErr := errors.New("custom error")
		called := false
		m := &clitest.MockCallbackNotifier{
			NotifyFunc: func(ctx context.Context, url string, headers map[string]string, payload domain_callback.CallbackPayload) error {
				called = true
				return customErr
			},
		}
		err := m.Notify(context.Background(), "http://localhost", nil, domain_callback.CallbackPayload{})
		if !errors.Is(err, customErr) {
			t.Errorf("expected %v, got %v", customErr, err)
		}
		if !called {
			t.Error("expected NotifyFunc to be called")
		}
	})
}

func TestMockModeLocker(t *testing.T) {
	t.Run("default nil TryLockModeFunc returns non-nil release func and nil error", func(t *testing.T) {
		m := &clitest.MockModeLocker{}
		release, err := m.TryLockMode("test")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if release == nil {
			t.Fatal("expected non-nil release func")
		}
		release()
	})

	t.Run("delegates to TryLockModeFunc", func(t *testing.T) {
		released := false
		m := &clitest.MockModeLocker{
			TryLockModeFunc: func(mode string) (func(), error) {
				if mode == "locked" {
					return nil, domain_persistence.ErrModeLocked
				}
				return func() { released = true }, nil
			},
		}

		_, err := m.TryLockMode("locked")
		if !errors.Is(err, domain_persistence.ErrModeLocked) {
			t.Errorf("expected ErrModeLocked, got %v", err)
		}

		release, err := m.TryLockMode("unlocked")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		release()
		if !released {
			t.Error("expected release func to be executed")
		}
	})
}
