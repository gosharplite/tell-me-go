// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package configtest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/stretchr/testify/mock"
)

// MockConfigLoader is a testify-based test double for config.ConfigLoader.
// Defensive nil handling lets tests call Return(nil, err) without
// triggering a nil-pointer assertion in the type assertion.
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
