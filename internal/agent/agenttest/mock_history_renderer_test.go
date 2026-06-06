// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"bytes"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockHistoryRenderer_Render(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := ports.HistoryRenderOptions{}

	m := new(MockHistoryRenderer)
	m.On("Render", &buf, nil, 0, opts).Return()

	m.Render(&buf, nil, 0, opts)
	m.AssertCalled(t, "Render", &buf, nil, 0, opts)
}
