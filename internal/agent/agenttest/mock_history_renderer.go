// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"io"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// MockHistoryRenderer is a testify-based test double for
// ports.HistoryRenderer. For tests that need a no-op stub rather than
// a recording mock, see stubHistoryRenderer in helpers.go.
type MockHistoryRenderer struct {
	mock.Mock
}

func (m *MockHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	m.Called(w, h, n, options)
}
