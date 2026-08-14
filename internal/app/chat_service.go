// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// NewChatService constructs the concrete chat service. The concrete
// implementation (internal/agent/chatService) stays in internal/agent; app is
// the construction seam so the di composition root never references
// internal/agent directly (issue #1364).
func NewChatService(cfg ports.ChatServiceConfig) ports.ChatService {
	return agent.NewChatService(cfg)
}
