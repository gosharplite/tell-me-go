package executor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Ensure CircuitBreakerPipeline implements ToolPipeline
var _ ToolPipeline = (*CircuitBreakerPipeline)(nil)

type circuitState int

const (
	stateClosed circuitState = iota
	stateOpen
	stateHalfOpen
)

// toolCircuit manages the state for a single tool's circuit breaker
type toolCircuit struct {
	name         string
	mu           sync.RWMutex
	failures     int
	state        circuitState
	lastFailure  time.Time
	threshold    int
	resetTimeout time.Duration
}

func newToolCircuit(name string, threshold int, resetTimeout time.Duration) *toolCircuit {
	return &toolCircuit{
		name:         name,
		threshold:    threshold,
		resetTimeout: resetTimeout,
		state:        stateClosed,
	}
}

func (c *toolCircuit) allowRequest() error {
	c.mu.RLock()
	state := c.state
	last := c.lastFailure
	timeout := c.resetTimeout
	c.mu.RUnlock()

	if state == stateOpen {
		if time.Since(last) > timeout {
			// Transition to half-open
			c.mu.Lock()
			if c.state == stateOpen {
				c.state = stateHalfOpen
			}
			c.mu.Unlock()
			return nil
		}
		return fmt.Errorf("%w: tool %q is temporarily disabled due to multiple consecutive failures", tools.ErrToolCircuitOpen, c.name)
	}
	return nil
}

func (c *toolCircuit) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.state = stateClosed
}

func (c *toolCircuit) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	c.lastFailure = time.Now()
	if c.failures >= c.threshold {
		c.state = stateOpen
	}
}

// CircuitBreakerPipeline wraps a ToolPipeline and applies circuit breaking logic per tool.
type CircuitBreakerPipeline struct {
	next         ToolPipeline
	threshold    int
	resetTimeout time.Duration
	circuits     sync.Map // map[string]*toolCircuit
}

// NewCircuitBreakerPipeline creates a new CircuitBreakerPipeline.
func NewCircuitBreakerPipeline(next ToolPipeline, threshold int, resetTimeout time.Duration) *CircuitBreakerPipeline {
	return &CircuitBreakerPipeline{
		next:         next,
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

func (c *CircuitBreakerPipeline) getCircuit(toolName string) *toolCircuit {
	if val, ok := c.circuits.Load(toolName); ok {
		return val.(*toolCircuit)
	}
	cb := newToolCircuit(toolName, c.threshold, c.resetTimeout)
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
