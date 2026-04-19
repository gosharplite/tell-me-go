<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Configuration Guide

This document covers configuration concerns that need more explanation than
fits in inline YAML comments. For the full annotated example, see the
**Configuration** section of the [README](../../README.md).

## Context-Window Headroom

Every LLM request must satisfy:

```text
prompt_tokens + completion_tokens  ≤  CONTEXT_WINDOW
```

`tell-me-go` enforces this client-side via `MAX_HISTORY_TOKENS:` in each YAML
config under `configs/`. The value caps how much conversation history is
replayed to the model on each turn, leaving room for the response (and, for
reasoning models, the thinking budget).

### The headroom rule

```text
MAX_HISTORY_TOKENS  =  CONTEXT_WINDOW − OUTPUT_BUDGET − safety_margin

where:
  CONTEXT_WINDOW  =  the active model's window, declared in MODELS: of the
                     same YAML file.
  OUTPUT_BUDGET   =  the active provider's THINKING_BUDGET if set,
                     otherwise its MAX_TOKENS, otherwise the SDK default
                     (16384). For Gemini providers, THINKING_BUDGET counts
                     against the output allocation per Google's API contract,
                     so it must be reserved here.
  safety_margin   =  10000 (flat).
                     Absorbs tokenizer drift — client-side token counts are
                     estimates, and server-side counts (especially Gemini and
                     DeepSeek) can differ by a few percent.
```

Worked example for the shipped configs (all five role files):

```text
SELECTED_PROVIDER  = google
google.THINKING_BUDGET = 32768
gemini-3-flash-preview.CONTEXT_WINDOW = 200000

MAX_HISTORY_TOKENS = 200000 − 32768 − 10000  ≈  157000   ✅ (rounded down)
```

### ⚠️ Hazard: switching `SELECTED_PROVIDER`

`MAX_HISTORY_TOKENS` is **not** auto-derived from the active provider — it
is a static value chosen for the provider that was active when the config
was last reviewed. Switching providers can silently break the budget:

| From → To                                  | Window change | Required action                                              |
| ------------------------------------------ | ------------- | ------------------------------------------------------------ |
| `google` (200K) → `deepseek` (128K)        | shrinks 72K   | **Lower** `MAX_HISTORY_TOKENS` to fit the smaller window     |
| `google` (200K) → `vertex-deepseek` (164K) | shrinks 36K   | **Lower** `MAX_HISTORY_TOKENS` accordingly                   |
| `deepseek` (128K) → `vertex-deepseek` (164K) | grows 36K   | Optional: raise `MAX_HISTORY_TOKENS` to use the extra room   |
| `google` (200K) → `claude` (200K)          | unchanged     | None (output budgets are similar)                            |

After changing `SELECTED_PROVIDER:`, recompute and update `MAX_HISTORY_TOKENS:`
using the rule above. The inline comment immediately above each
`MAX_HISTORY_TOKENS:` in `configs/*.yaml` records the formula applied for that
file's currently-selected provider.

### Symptoms of an over-budget config

- HTTP `400 context_length_exceeded` (or the provider-specific equivalent) on
  the first turn that fills history.
- Silent truncation surfaced as `finish_reason: "length"` when the request
  is accepted but the model has too few output tokens left to complete a
  response.

If you see either symptom after switching providers, the most likely cause is
a stale `MAX_HISTORY_TOKENS:` value.
