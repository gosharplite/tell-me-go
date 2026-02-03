<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Economic Awareness & Loop Protection

## Overview
The "Economic Awareness" milestone implements cost monitoring and loop detection to ensure the agent is "Safe-by-Design" regarding resource consumption.

## Core Components

### 1. Cost Tracking (`internal/tools/framework/metrics.go`)
- **Event-Driven**: The `TurnEngine` and `Agent` do not calculate costs directly. Instead, they publish `UsageMetricsEvent` containing `llm.Metrics`.
- **SessionCostTracker**: A central component (often used by `TurnEngine`) that subscribes to metrics events and calculates USD costs using `pricing.PricingData`.
- **Persistence**: Costs are logged to `tokens.log` and summarized in `history.json`.

### 2. Budget Enforcement (`internal/agent/turn_engine.go`)
- **HardBudgetLimit**: A `float64` field in `TurnEngine`.
- **checkLimits()**: A mandatory pre-turn check that compares the current session cost (from `SessionCostTracker`) against the `HardBudgetLimit`.
- **Thread Safety**: Access to budget and cost data is protected by `sync.RWMutex` to prevent data races during concurrent reconfigurations.

### 3. Loop Mitigation (`internal/agent/turn_engine.go`)
- **WithLoopDetector Middleware**: Intercepts model responses.
- **Full-Response Hashing**: Hashes the complete `llm.Response` JSON (Content + FunctionCalls). This captures loops where the text might change slightly but the tool calls are identical.
- **Tool-Call Counter**: Tracks `tool_name + arguments` frequency.

## Implementation Standards
- **Safe-by-Design**: Budget limits are internal-only by default (disabled at 0.0) to maintain a clean UI, but can be set via API.
- **Deterministic**: Loop detection uses cryptographic hashes (SHA-256) to ensure consistent detection across environments.
