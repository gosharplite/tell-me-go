// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"google.golang.org/genai"
)

func (c *Client) prepareRequest(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*genai.GenerateContentConfig, []*genai.Content) {
	filteredHistory := make([]*llm.Content, 0, len(history))
	dynamicSystemParts := make([]*llm.Part, 0, len(history)) // Upper bound

	// 1. Separate system instructions from the standard conversation history
	for _, h := range history {
		if h.Role == "system" {
			dynamicSystemParts = append(dynamicSystemParts, h.Parts...)
			continue
		}
		filteredHistory = append(filteredHistory, h)
	}

	// 2. Get baseline tools and the static configured system instruction
	activeTools, systemInstruction := c.configureTools(ctx, tools, resolver)

	// 3. Merge any dynamically injected system prompts (e.g., Skills)
	if len(dynamicSystemParts) > 0 {
		if systemInstruction == nil {
			systemInstruction = &genai.Content{Role: "system"}
		}

		// Convert dynamic parts to SDK format using the package-level adapter function
		dynamicContent := &llm.Content{Role: "system", Parts: dynamicSystemParts}
		sdkDynamic := toSDKContent(ctx, dynamicContent, resolver)
		if sdkDynamic != nil {
			systemInstruction.Parts = append(systemInstruction.Parts, sdkDynamic.Parts...)
		}
	}

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: systemInstruction,
	}

	// Apply the per-request output-token budget. See defaultMaxOutputTokens
	// and truncation_test.go for the rationale (silent tool-call
	// truncation when the API's implicit default is hit). c.maxOutputTokens
	// is initialized in NewClient to defaultMaxOutputTokens and may be
	// overridden via WithMaxOutputTokens.
	c.mu.RLock()
	maxOut := c.maxOutputTokens
	c.mu.RUnlock()
	if maxOut > 0 {
		config.MaxOutputTokens = int32(maxOut)
	}

	c.configureThinking(ctx, config)

	// 4. Return the config and the filtered history containing ONLY user/model roles
	return config, c.toSDKContent(ctx, filteredHistory, resolver)
}

func (c *Client) configureTools(ctx context.Context, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) ([]*genai.Tool, *genai.Content) {
	c.mu.RLock()
	useSearch := c.useSearch
	instr := c.systemInstruction
	c.mu.RUnlock()

	// Add Search tool
	var activeTools []*genai.Tool
	activeTools = append(activeTools, toSDKTool(tools)...)
	if useSearch {
		activeTools = append(activeTools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	return activeTools, toSDKContent(ctx, instr, resolver)
}

func (c *Client) configureThinking(ctx context.Context, config *genai.GenerateContentConfig) {
	c.mu.RLock()
	level := c.thinkingLevel
	budget := c.thinkingBudget
	maxBudget := c.maxThinkingBudget
	model := c.model
	c.mu.RUnlock()

	if level == "" && budget <= 0 {
		return
	}

	config.ThinkingConfig = &genai.ThinkingConfig{
		IncludeThoughts: true,
	}

	if budget > 0 {
		c.applyThinkingBudget(ctx, config.ThinkingConfig, budget, maxBudget, model)
	} else if level != "" {
		config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(level)
	}
}

func (c *Client) applyThinkingBudget(ctx context.Context, config *genai.ThinkingConfig, budget, maxBudget int, model string) {
	actualBudget := budget
	if maxBudget > 0 && actualBudget > maxBudget {
		evt := events.SystemMessageEvent{
			Message: fmt.Sprintf("Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.", actualBudget, model, maxBudget, maxBudget),
			Level:   "warning",
		}
		if err := events.SafePublish(ctx, c.eventBus, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				c.logger.Error("event_publish_failed",
					"event_type", string(evt.Type()),
					"error", err)
			}
		}
		actualBudget = maxBudget
	}
	config.ThinkingBudget = genai.Ptr(int32(actualBudget))
}

func (c *Client) toSDKContent(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) []*genai.Content {
	sdkHistory := make([]*genai.Content, 0, len(history))
	for _, h := range history {
		sdkContent := toSDKContent(ctx, h, resolver)
		if sdkContent == nil {
			continue
		}
		// Defensive check: Ensure all content objects have at least one part for the SDK.
		// NOTE: ContextManager should have already filtered out truly empty turns.
		if len(sdkContent.Parts) == 0 {
			sdkContent.Parts = []*genai.Part{{Text: "[empty]"}}
		}
		sdkHistory = append(sdkHistory, sdkContent)
	}
	return sdkHistory
}
