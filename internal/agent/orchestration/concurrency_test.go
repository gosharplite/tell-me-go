// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingTransformer struct {
	priority int
	block    chan struct{}
}

func (t *blockingTransformer) Transform(ctx context.Context, req *services.ContextRequest) error {
	// Signal we want history persisted so ExecuteWithPersistence calls persistFn
	req.PersistHistory = true
	if t.block != nil {
		<-t.block
	}
	return nil
}

func (t *blockingTransformer) Priority() int {
	return t.priority
}

type noopTransformer struct {
	priority int
}

func (t *noopTransformer) Transform(ctx context.Context, req *services.ContextRequest) error {
	return nil
}

func (t *noopTransformer) Priority() int {
	return t.priority
}

func TestContextManager_Prepare_ConcurrencyDetection(t *testing.T) {
	// 1. Setup CM with a blocking transformer (Priority 50)
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	history := &mockHistoryManager{}

	blockCh := make(chan struct{})
	bt := &blockingTransformer{priority: 50, block: blockCh}
	// Add a transformer with Priority >= 100 to trigger persistence
	// The pipeline calls persistFn when it transitions from Priority < 100 to Priority >= 100
	nt := &noopTransformer{priority: 150}

	pipeline := NewContextPipeline(bt, nt)
	cm := NewContextManager(strategy, history, nil, nil)
	cm.SetPipeline(pipeline)

	// 2. Start cm.Prepare(...) in goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var prepareErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, prepareErr = cm.Prepare(ctx, 1)
	}()

	// Give the goroutine a moment to start and hit the block
	// We want to ensure it's inside ExecuteWithPersistence, specifically blocked in bt.Transform
	time.Sleep(100 * time.Millisecond)

	// 3. Call cm.AddContent(...) to bump version
	// This will increment cm.version
	err := cm.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "concurrent update"}}})
	require.NoError(t, err)

	// 4. Release blocking transformer
	// This will allow the pipeline to proceed to nt, triggering persistFn
	close(blockCh)
	wg.Wait()

	// 5. Assert result is ErrTransient and contains the correct message
	require.Error(t, prepareErr)
	assert.Contains(t, prepareErr.Error(), "concurrent history modification detected")
}
