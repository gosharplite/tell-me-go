// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
)

var _ domain_callback.CallbackNotifier = (*MockCallbackNotifier)(nil)

type MockCallbackNotifier struct {
	NotifyFunc func(ctx context.Context, url string, headers map[string]string, payload domain_callback.CallbackPayload) error
}

func (m *MockCallbackNotifier) Notify(ctx context.Context, url string, headers map[string]string, payload domain_callback.CallbackPayload) error {
	if m.NotifyFunc != nil {
		return m.NotifyFunc(ctx, url, headers, payload)
	}
	return nil
}
