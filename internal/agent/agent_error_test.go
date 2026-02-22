package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
)

func TestAgent_ConfigFailure(t *testing.T) {
	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	
	hm := &mockHistoryManager{
		AddContentFunc: func(c context.Context, content *llm.Content) error {
			// Cancel context right after AddContent succeeds so applyConfig fails
			cancel()
			return nil
		},
	}
	
	a := &agent{
		events: events.NewSimpleEventBus(),
		ctxManager: &orchestration.ContextManager{
			History: hm,
		},
	}
	
	session := &services.Session{StartTime: time.Now()}
	err := a.Chat(ctx, session, "hello")
	
	if err == nil {
		t.Fatal("Expected error due to config failure/context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error from applyConfig, got: %v", err)
	}
}
