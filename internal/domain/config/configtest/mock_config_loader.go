// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package configtest

import "github.com/gosharplite/tell-me-go/internal/domain/config"

// MockConfigLoader is a hand-rolled mock for config.ConfigLoader.
// Set LoadFunc to override behavior; when nil, Load returns (nil, nil).
type MockConfigLoader struct {
	// LoadFunc is invoked by Load when non-nil.
	LoadFunc func(path string) (*config.Config, error)
}

func (m *MockConfigLoader) Load(path string) (*config.Config, error) {
	if m.LoadFunc != nil {
		return m.LoadFunc(path)
	}
	return nil, nil
}
