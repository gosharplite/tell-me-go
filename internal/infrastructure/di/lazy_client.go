package di

import (
	stdctx "context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// LazyClient wraps an LLM client factory and initializes the underlying
// client on first use. It implements llm.ExtendedClient directly.
type LazyClient struct {
	once    sync.Once
	err     error
	client  llm.ExtendedClient
	factory func() (llm.ExtendedClient, error)
}

// NewLazyClient creates a LazyClient backed by the given factory function.
func NewLazyClient(factory func() (llm.ExtendedClient, error)) *LazyClient {
	return &LazyClient{factory: factory}
}

func (lc *LazyClient) init() {
	lc.once.Do(func() {
		lc.client, lc.err = lc.factory()
	})
}

// Init explicitly triggers initialization and returns any error.
// Useful for health checks that need to force early initialization.
func (lc *LazyClient) Init() error {
	lc.init()
	return lc.err
}

func (lc *LazyClient) Generate(ctx stdctx.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	lc.init()
	if lc.err != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.Generate(ctx, input, tools, resolver)
}

func (lc *LazyClient) SendChat(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	lc.init()
	if lc.err != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.SendChat(ctx, history, tools, resolver)
}

func (lc *LazyClient) GenerateImages(ctx stdctx.Context, model, prompt string, mimeType string) ([][]byte, error) {
	lc.init()
	if lc.err != nil {
		return nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.GenerateImages(ctx, model, prompt, mimeType)
}

func (lc *LazyClient) RefreshAuth() error {
	lc.init()
	if lc.err != nil {
		return fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.RefreshAuth()
}

// Ensure LazyClient satisfies llm.ExtendedClient at compile time.
var _ llm.ExtendedClient = (*LazyClient)(nil)
