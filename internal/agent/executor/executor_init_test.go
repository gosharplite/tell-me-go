package executor

import (
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultToolPipeline(t *testing.T) {
	tests := []struct {
		name     string
		registry tools.Registry
		sm       domain_security.Manager
		bus      events.EventBus
		logger   ports.Logger
		zombie   *tools.ZombieTool
		wantErr  bool // Not returned by NewDefaultToolPipeline but for structure
	}{
		{
			name:     "valid dependencies initializes successfully",
			registry: &mockToolRegistry{},
			sm:       &mockSecurityManager{},
			bus:      &mockEventBus{},
			logger:   &ports.NoOpLogger{},
			zombie:   &tools.ZombieTool{},
			wantErr:  false,
		},
		{
			name:     "nil dependencies still initialize (though not recommended)",
			registry: nil,
			sm:       nil,
			bus:      nil,
			logger:   nil,
			zombie:   nil,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := NewDefaultToolPipeline(
				tt.registry,
				tt.sm,
				tt.bus,
				tt.logger,
				tt.zombie,
				time.Second,
				time.Second,
				time.Second,
			)

			if tt.wantErr {
				assert.Nil(t, pipeline)
				return
			}

			assert.NotNil(t, pipeline)

			// Type assert to verify internal fields
			defaultPipeline, ok := pipeline.(*defaultToolPipeline)
			assert.True(t, ok)
			assert.NotNil(t, defaultPipeline.resolver)
			assert.NotNil(t, defaultPipeline.authorizer)
			assert.NotNil(t, defaultPipeline.runtime)
			assert.Equal(t, tt.registry, defaultPipeline.registry)
		})
	}
}
