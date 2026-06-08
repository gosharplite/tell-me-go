// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"bytes"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockHistoryRenderer_Render(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := ports.HistoryRenderOptions{}
	m := &MockHistoryRenderer{}

	m.Render(&buf, nil, 0, opts)

	calls, methods := m.Snapshot()
	if calls != 1 {
		t.Errorf("Render calls: got %d, want 1", calls)
	}
	if len(methods) != 1 || methods[0] != "Render" {
		t.Errorf("methods: got %v, want [Render]", methods)
	}
}

func TestMockHistoryRenderer_RaceDetection(t *testing.T) {
	m := &MockHistoryRenderer{}
	var buf bytes.Buffer
	opts := ports.HistoryRenderOptions{}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.Render(&buf, nil, 0, opts)
			}
		}()
	}
	wg.Wait()

	calls, _ := m.Snapshot()
	if calls != goroutines*iterations {
		t.Errorf("Render calls: got %d, want %d", calls, goroutines*iterations)
	}
}
