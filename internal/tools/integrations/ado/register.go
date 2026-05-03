// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// marshalIndentResult marshals an arbitrary value to indented JSON and wraps
// it in a tools.ToolResult for handlers that return raw structs/slices.
func marshalIndentResult(v interface{}) (tools.ToolResult, error) {
	output, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("marshaling response: %w", err)
	}
	return tools.ToolResult{Text: string(output)}, nil
}

// appendTruncationNote appends a truncation advisory if the log content was
// clipped after TotalLines lines, helping the LLM understand the data may be
// incomplete.
func appendTruncationNote(c logContent) string {
	if !c.Truncated {
		return c.Content
	}
	return c.Content + fmt.Sprintf("\n\n[NOTE to LLM: Log content was truncated after %d lines for safety. Suggest using a more specific filter_query or pagination if the relevant data is missing.]", c.TotalLines)
}

// Register registers all Azure DevOps tools into the provided registry.
func Register(r tools.Registry, sm security.Manager, client tools.HTTPClient) error {
	var opts []AdoOption
	if client != nil {
		opts = append(opts, WithHTTPClient(client))
	}
	if token := os.Getenv("AZURE_PAT_ALL"); token != "" {
		opts = append(opts, WithToken(token))
	}
	m := NewADOManager(sm, opts...)
	f := newPipelineFormatter()

	for _, fn := range []func(tools.Registry, *AdoManager, PipelineFormatter) error{
		registerPullRequests,
		registerPipelines,
		registerBuilds,
		registerRepository,
		registerPolicy,
	} {
		if err := fn(r, m, f); err != nil {
			return err
		}
	}
	return nil
}
