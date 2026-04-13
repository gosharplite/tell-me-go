// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/stretchr/testify/mock"
)

// MockConfigLoader is a mock implementation of config.ConfigLoader for testing.
type MockConfigLoader struct {
	mock.Mock
}

func (m *MockConfigLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

// MockSessionLoader is a mock implementation of config.SessionLoader for testing.
type MockSessionLoader struct {
	mock.Mock
}

func (m *MockSessionLoader) LoadSession(path string) (*config.SessionConfig, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.SessionConfig), args.Error(1)
}
