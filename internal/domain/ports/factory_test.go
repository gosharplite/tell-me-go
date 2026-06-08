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

	tests := []struct {
		name      string
		factory   ClientFactoryFunc
		wantNil   bool
		wantErr   bool
		errTarget error
	}{
		{
			name: "success: returns client and nil error",
			factory: func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
				return nil, nil
			},
			wantNil: true,
			wantErr: false,
		},
		{
			name: "error: returns nil client and error",
			factory: func(cfg *config.Config, pd pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
				return nil, ErrHistoryNotFound
			},
			wantNil:   true,
			wantErr:   true,
			errTarget: ErrHistoryNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := tt.factory.NewClient(cfg, pd, bus, logger)

			if tt.wantNil && client != nil {
				t.Errorf("expected nil client, got %+v", client)
			}
			if !tt.wantNil && client == nil {
				t.Error("expected non-nil client, got nil")
			}
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.errTarget != nil && err != tt.errTarget {
				t.Errorf("expected error %v, got %v", tt.errTarget, err)
			}
		})
	}
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
