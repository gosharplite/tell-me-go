package executor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// Ensure CircuitBreakerPipeline implements ToolPipeline
var _ ToolPipeline = (*CircuitBreakerPipeline)(nil)

type CircuitState int32

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// toolCircuit manages the state for a single tool's circuit breaker
type toolCircuit struct {
	name         string
	transitionMu sync.Mutex
	failures     atomic.Int64
	state        atomic.Int32
	openedAt     atomic.Int64
	threshold    int
	resetTimeout time.Duration
	clock        clock.Clock
}

func newToolCircuit(name string, threshold int, resetTimeout time.Duration, clk clock.Clock) *toolCircuit {
	if clk == nil {
		clk = clock.RealClock{}
	}
	c := &toolCircuit{
		name:         name,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		clock:        clk,
	}
	c.state.Store(int32(StateClosed))
	return c
}

func (c *toolCircuit) allowRequest() error {
	state := c.state.Load()

	if state == int32(StateOpen) {
		openedAtUnix := c.openedAt.Load()
		openedAt := time.Unix(0, openedAtUnix)
		if c.clock.Since(openedAt) > c.resetTimeout {
			// Try to transition to half-open
			if c.tryTransitionToHalfOpen() {
				return nil // This is the chosen, single probe request
			}
		}
		return fmt.Errorf("%w: tool %q is temporarily disabled due to multiple consecutive failures", tools.ErrToolCircuitOpen, c.name)
	}

	if state == int32(StateHalfOpen) {
		// REJECT all concurrent requests while the single elected probe is in flight
		return fmt.Errorf("%w: tool %q is currently probing", tools.ErrToolCircuitOpen, c.name)
	}

	// StateClosed
	return nil
}

func (c *toolCircuit) tryTransitionToHalfOpen() bool {
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()

	if c.state.Load() == int32(StateOpen) {
		c.state.Store(int32(StateHalfOpen))
		return true
	}
	return false
}

func (c *toolCircuit) recordSuccess() {
	if c.state.Load() != int32(StateClosed) {
		c.resetToClosed()
	} else {
		// Just clear the failures counter in the fast path
		c.failures.Store(0)
	}
}

func (c *toolCircuit) resetToClosed() {
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()

	if c.state.Load() != int32(StateClosed) {
		c.state.Store(int32(StateClosed))
		c.failures.Store(0)
	}
}

func (c *toolCircuit) recordFailure() {
	count := c.failures.Add(1)

	state := c.state.Load()
	if (state == int32(StateClosed) && count >= int64(c.threshold)) || state == int32(StateHalfOpen) {
		c.tripToOpen()
	}
}

func (c *toolCircuit) tripToOpen() {
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()

	state := c.state.Load()
	if state == int32(StateClosed) || state == int32(StateHalfOpen) {
		c.state.Store(int32(StateOpen))
		c.openedAt.Store(c.clock.Now().UnixNano())
		c.failures.Store(0) // Explicitly clear counters!
	}
}

// CircuitBreakerPipeline wraps a ToolPipeline and applies circuit breaking logic per tool.
type CircuitBreakerPipeline struct {
	next         ToolPipeline
	threshold    int
	resetTimeout time.Duration
	circuits     sync.Map // map[string]*toolCircuit
	clock        clock.Clock
}

// CircuitBreakerOption defines functional options for CircuitBreakerPipeline.
type CircuitBreakerOption func(*CircuitBreakerPipeline)

// WithClock injects a custom clock.
func WithClock(clk clock.Clock) CircuitBreakerOption {
	return func(c *CircuitBreakerPipeline) {
		c.clock = clk
	}
}

// NewCircuitBreakerPipeline creates a new CircuitBreakerPipeline.
func NewCircuitBreakerPipeline(next ToolPipeline, threshold int, resetTimeout time.Duration, opts ...CircuitBreakerOption) *CircuitBreakerPipeline {
	c := &CircuitBreakerPipeline{
		next:         next,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		clock:        clock.RealClock{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *CircuitBreakerPipeline) getCircuit(toolName string) *toolCircuit {
	if val, ok := c.circuits.Load(toolName); ok {
		return val.(*toolCircuit)
	}
	cb := newToolCircuit(toolName, c.threshold, c.resetTimeout, c.clock)
	val, _ := c.circuits.LoadOrStore(toolName, cb)
	return val.(*toolCircuit)
}

// ExecuteTool wraps the underlying pipeline execution with circuit breaker logic.
func (c *CircuitBreakerPipeline) ExecuteTool(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
	circuit := c.getCircuit(call.Name)

	if err := circuit.allowRequest(); err != nil {
		return tools.ToolResult{
			Text:  err.Error(),
			Error: err,
		}
	}

	result := c.next.ExecuteTool(ctx, call)

	// A tool execution is considered a failure if it returns a non-nil Error.
	if result.Error != nil {
		circuit.recordFailure()
	} else {
		circuit.recordSuccess()
	}

	return result
}

// RequestBatchConsent passes the request to the underlying pipeline.
func (c *CircuitBreakerPipeline) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	return c.next.RequestBatchConsent(ctx, calls)
}

// IsSerial passes the request to the underlying pipeline.
func (c *CircuitBreakerPipeline) IsSerial(toolName string) bool {
	return c.next.IsSerial(toolName)
}
