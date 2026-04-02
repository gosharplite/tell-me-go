package executor

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestRequestBatchConsent(t *testing.T) {
	tests := []struct {
		name       string
		calls      []*llm.FunctionCall
		wantCtx    context.Context // Just basic check
		wantResult map[int]bool
	}{
		{
			name:       "nil inputs",
			calls:      nil,
			wantResult: map[int]bool{},
		},
		{
			name:       "empty batches",
			calls:      []*llm.FunctionCall{},
			wantResult: map[int]bool{},
		},
		{
			name: "valid calls",
			calls: []*llm.FunctionCall{
				{Name: "tool1"},
				{Name: "tool2"},
			},
			wantResult: map[int]bool{},
		},
	}

	authorizer := &MockPipelineAuthorizer{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			gotCtx, gotResult := authorizer.RequestBatchConsent(ctx, tt.calls)
			
			assert.NotNil(t, gotCtx)
			assert.Equal(t, tt.wantResult, gotResult)
		})
	}
}

func TestBuildOrchestrator(t *testing.T) {
	tests := []struct {
		name       string
		registry   tools.Registry
		sm         domain_security.Manager
		bus        events.EventBus
		logger     ports.Logger
		observer   tools.ExecutionObserver
		opts       []executorOption
		wantErr    bool
	}{
		{
			name:     "valid setup",
			registry: &mockToolRegistry{},
			sm:       &mockSecurityManager{},
			bus:      &mockEventBus{},
			logger:   &ports.NoOpLogger{},
			observer: &MockLogger{},
			wantErr:  false,
		},
		{
			name:     "missing registry",
			registry: nil,
			sm:       &mockSecurityManager{},
			bus:      &mockEventBus{},
			logger:   &ports.NoOpLogger{},
			observer: &MockLogger{},
			wantErr:  true,
		},
		{
			name:     "missing logger",
			registry: &mockToolRegistry{},
			sm:       &mockSecurityManager{},
			bus:      &mockEventBus{},
			logger:   nil,
			observer: &MockLogger{},
			wantErr:  true,
		},
		{
			name:     "missing observer",
			registry: &mockToolRegistry{},
			sm:       &mockSecurityManager{},
			bus:      &mockEventBus{},
			logger:   &ports.NoOpLogger{},
			observer: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch, err := BuildOrchestrator(tt.registry, tt.sm, tt.bus, tt.logger, tt.observer, tt.opts...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, orch)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, orch)
			assert.NotNil(t, orch.pipeline)
			
			state := orch.state.Load()
			assert.NotNil(t, state)
			assert.Equal(t, 5, state.config.MaxConcurrentTools) // Default check
			
			orch.Shutdown() // Avoid goleak
		})
	}
}
