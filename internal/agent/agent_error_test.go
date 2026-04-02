package agent

import (
	"context"
	"errors"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestAgent_ConfigFailure(t *testing.T) {
	t.Parallel()
	// Create a context that we can cancel

	ctx, cancel := context.WithCancel(context.Background())

	hm := &mockHistoryManager{
		AddContentFunc: func(c context.Context, content *llm.Content) error {
			// Cancel context right after AddContent succeeds so applyConfig fails
			cancel()
			return nil
		},
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	a := &agent{
		events: bus,
		ctxManager: &orchestration.ContextManager{
			History: hm,
		},
	}

	session := &ports.Session{StartTime: time.Now()}
	err := a.Chat(ctx, session, "hello")

	if err == nil {
		t.Fatal("Expected error due to config failure/context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error from applyConfig, got: %v", err)
	}
}
