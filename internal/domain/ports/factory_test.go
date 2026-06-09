// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestClientFactoryFunc_NewClient(t *testing.T) {
	t.Parallel()

	// nilDeps provides zero-value arguments for the factory call.
	// ClientFactoryFunc implementations ignore these in test.
	var (
		cfg    *config.Config
		pd     pricing.PricingData
		bus    events.EventBus
		logger Logger
	)

	t.Run("success: returns client and nil error", func(t *testing.T) {
		factory := ClientFactoryFunc(func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
			return nil, nil
		})
		client, err := factory.NewClient(cfg, pd, bus, logger)

		if client != nil {
			t.Errorf("expected nil client, got %+v", client)
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error: returns nil client and error", func(t *testing.T) {
		factory := ClientFactoryFunc(func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
			return nil, ErrHistoryNotFound
		})
		client, err := factory.NewClient(cfg, pd, bus, logger)

		if client != nil {
			t.Errorf("expected nil client, got %+v", client)
		}
		if err == nil {
			t.Error("expected error, got nil")
		}
		if err != ErrHistoryNotFound {
			t.Errorf("expected error %v, got %v", ErrHistoryNotFound, err)
		}
	})
}

func TestClientFactoryFunc_NewFailoverChain(t *testing.T) {
	t.Parallel()

	var (
		cfg    *config.Config
		pd     pricing.PricingData
		bus    events.EventBus
		logger Logger
	)

	tests := []struct {
		name    string
		factory ClientFactoryFunc
	}{
		{
			name: "returns nil, nil regardless of factory success",
			factory: func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
				return nil, nil // would-be success
			},
		},
		{
			name: "returns nil, nil regardless of factory error",
			factory: func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
				return nil, ErrHistoryNotFound // would-be error
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := tt.factory.NewFailoverChain(cfg, pd, bus, logger)

			if client != nil {
				t.Errorf("NewFailoverChain must return nil client, got %+v", client)
			}
			if err != nil {
				t.Errorf("NewFailoverChain must return nil error, got %v", err)
			}
		})
	}
}
