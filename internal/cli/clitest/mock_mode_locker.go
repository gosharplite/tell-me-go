// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

var _ domain_persistence.ModeLocker = (*MockModeLocker)(nil)

type MockModeLocker struct {
	TryLockModeFunc func(mode string) (func(), error)
}

func (m *MockModeLocker) TryLockMode(mode string) (func(), error) {
	if m.TryLockModeFunc != nil {
		return m.TryLockModeFunc(mode)
	}
	return func() {}, nil
}
