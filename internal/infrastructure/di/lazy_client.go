package di

import (
	stdctx "context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// lazyClient wraps an LLM client factory and initializes the underlying
// client on first use. It implements llm.ExtendedClient directly.
type lazyClient struct {
	once    sync.Once
	err     error
	client  llm.ExtendedClient
	factory func() (llm.ExtendedClient, error)
}

// newLazyClient creates a lazyClient backed by the given factory function.
func newLazyClient(factory func() (llm.ExtendedClient, error)) *lazyClient {
	return &lazyClient{factory: factory}
}

func (lc *lazyClient) init() {
	lc.once.Do(func() {
		lc.client, lc.err = lc.factory()
	})
}

func (lc *lazyClient) Generate(ctx stdctx.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	lc.init()
	if lc.err != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.Generate(ctx, input, tools, resolver)
}

func (lc *lazyClient) SendChat(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	lc.init()
	if lc.err != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.SendChat(ctx, history, tools, resolver)
}

func (lc *lazyClient) GenerateImages(ctx stdctx.Context, model, prompt string, mimeType string) ([][]byte, error) {
	lc.init()
	if lc.err != nil {
		return nil, fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.GenerateImages(ctx, model, prompt, mimeType)
}

func (lc *lazyClient) RefreshAuth() error {
	lc.init()
	if lc.err != nil {
		return fmt.Errorf("LLM provider initialization failed: %w", lc.err)
	}
	return lc.client.RefreshAuth()
}

// Ensure LazyClient satisfies llm.ExtendedClient at compile time.
var _ llm.ExtendedClient = (*lazyClient)(nil)
