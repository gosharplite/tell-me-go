// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ---------------------------------------------------------------------------
// Stub registry
// ---------------------------------------------------------------------------

// stubRegistry implements tools.Registry. Only GetDeclarations returns
// meaningful data; all other methods panic (Count never calls them).
type stubRegistry struct {
	decls []*tools.ToolDeclaration
}

func (s *stubRegistry) GetDeclarations() []*tools.ToolDeclaration { return s.decls }

func (s *stubRegistry) Register(_ *tools.ToolDeclaration, _ tools.ToolFunc) error {
	panic("not implemented")
}
func (s *stubRegistry) RegisterWithOptions(_ *tools.ToolDeclaration, _ tools.ToolFunc, _ tools.ToolOptions) error {
	panic("not implemented")
}
func (s *stubRegistry) RegisterToToolkit(_ string, _ *tools.ToolDeclaration, _ tools.ToolFunc) error {
	panic("not implemented")
}
func (s *stubRegistry) RegisterToToolkitWithOptions(_ string, _ *tools.ToolDeclaration, _ tools.ToolFunc, _ tools.ToolOptions) error {
	panic("not implemented")
}
func (s *stubRegistry) Execute(_ context.Context, _ string, _ map[string]interface{}, _ chan<- struct{}) (tools.ToolResult, error) {
	panic("not implemented")
}
func (s *stubRegistry) IsSerial(_ string) bool                        { panic("not implemented") }
func (s *stubRegistry) IsLongRunning(_ string) bool                   { panic("not implemented") }
func (s *stubRegistry) GetOptions(_ string) tools.ToolOptions         { panic("not implemented") }
func (s *stubRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { panic("not implemented") }
func (s *stubRegistry) GetDeclarationsByToolkits(_ []string) []*tools.ToolDeclaration {
	panic("not implemented")
}
func (s *stubRegistry) ListAvailableToolkits() []string { panic("not implemented") }

// ---------------------------------------------------------------------------
// Tool declaration builder
// ---------------------------------------------------------------------------

// makeToolDecls returns n realistic *tools.ToolDeclaration entries.
func makeToolDecls(n int) []*tools.ToolDeclaration {
	templates := []struct {
		name, desc string
		props      map[string]*tools.Schema
	}{
		{"read_file", "Reads the content of a file at the given path.", map[string]*tools.Schema{
			"path": {Type: "string", Description: "The file path to read."},
		}},
		{"write_file", "Writes content to a file, creating it if needed.", map[string]*tools.Schema{
			"path":    {Type: "string", Description: "The file path to write."},
			"content": {Type: "string", Description: "The content to write."},
		}},
		{"list_directory", "Lists files and directories at a given path.", map[string]*tools.Schema{
			"path":      {Type: "string", Description: "The directory path."},
			"recursive": {Type: "boolean", Description: "Recurse into subdirectories."},
		}},
		{"search_files", "Searches for a pattern across files.", map[string]*tools.Schema{
			"pattern": {Type: "string", Description: "The regex pattern."},
			"path":    {Type: "string", Description: "The directory to search in."},
		}},
		{"run_command", "Executes a shell command and returns output.", map[string]*tools.Schema{
			"command": {Type: "string", Description: "The command to execute."},
			"timeout": {Type: "number", Description: "Timeout in seconds."},
		}},
		{"web_search", "Performs a web search for the given query.", map[string]*tools.Schema{
			"query":       {Type: "string", Description: "The search query."},
			"num_results": {Type: "number", Description: "Number of results to return."},
		}},
		{"web_fetch", "Fetches the content of a URL and returns it.", map[string]*tools.Schema{
			"url":     {Type: "string", Description: "The URL to fetch."},
			"headers": {Type: "object", Description: "Optional headers."},
		}},
		{"git_diff", "Shows the git diff for the working tree.", map[string]*tools.Schema{
			"staged": {Type: "boolean", Description: "Show staged changes only."},
			"path":   {Type: "string", Description: "Limit to a specific path."},
		}},
		{"git_log", "Shows the git commit history.", map[string]*tools.Schema{
			"max_count": {Type: "number", Description: "Maximum number of commits."},
			"author":    {Type: "string", Description: "Filter by author."},
		}},
		{"run_tests", "Executes the project test suite.", map[string]*tools.Schema{
			"package": {Type: "string", Description: "The package path to test."},
			"verbose": {Type: "boolean", Description: "Enable verbose output."},
		}},
		{"format_code", "Formats source code according to language rules.", map[string]*tools.Schema{
			"path":    {Type: "string", Description: "Path to format."},
			"dry_run": {Type: "boolean", Description: "Preview changes without applying."},
		}},
		{"database_query", "Executes a read-only SQL query.", map[string]*tools.Schema{
			"sql":    {Type: "string", Description: "The SQL query."},
			"params": {Type: "array", Description: "Query parameters."},
		}},
		{"generate_image", "Generates an image from a text prompt.", map[string]*tools.Schema{
			"prompt": {Type: "string", Description: "The image description."},
			"size":   {Type: "string", Description: "Image dimensions (e.g. 1024x1024)."},
			"style":  {Type: "string", Description: "Art style for the image."},
		}},
		{"parse_document", "Parses a document and extracts structured data.", map[string]*tools.Schema{
			"path":   {Type: "string", Description: "The document path."},
			"format": {Type: "string", Description: "Output format (json, yaml, csv)."},
		}},
		{"send_email", "Sends an email via the configured SMTP server.", map[string]*tools.Schema{
			"to":      {Type: "string", Description: "Recipient address."},
			"subject": {Type: "string", Description: "Email subject."},
			"body":    {Type: "string", Description: "Email body."},
		}},
		{"schedule_task", "Schedules a task to run at a future time.", map[string]*tools.Schema{
			"task":      {Type: "string", Description: "Task description."},
			"cron_expr": {Type: "string", Description: "Cron expression."},
		}},
		{"manage_secrets", "Reads or writes secrets from the vault.", map[string]*tools.Schema{
			"key":   {Type: "string", Description: "Secret key."},
			"value": {Type: "string", Description: "Secret value (write only)."},
			"op":    {Type: "string", Description: "Operation: read or write."},
		}},
		{"deploy_service", "Triggers a deployment pipeline.", map[string]*tools.Schema{
			"service":  {Type: "string", Description: "Service name."},
			"version":  {Type: "string", Description: "Version tag."},
			"env":      {Type: "string", Description: "Target environment."},
			"rollback": {Type: "boolean", Description: "Perform rollback instead."},
		}},
		{"monitor_metrics", "Queries monitoring metrics for a service.", map[string]*tools.Schema{
			"metric": {Type: "string", Description: "Metric name."},
			"from":   {Type: "string", Description: "Start time (ISO 8601)."},
			"to":     {Type: "string", Description: "End time (ISO 8601)."},
		}},
		{"manage_feature_flags", "Reads or toggles feature flags.", map[string]*tools.Schema{
			"flag":    {Type: "string", Description: "Feature flag name."},
			"enabled": {Type: "boolean", Description: "Enable or disable the flag."},
			"scope":   {Type: "string", Description: "Scope (global, user, team)."},
		}},
	}

	decls := make([]*tools.ToolDeclaration, n)
	for i := 0; i < n; i++ {
		tmpl := templates[i%len(templates)]
		decls[i] = &tools.ToolDeclaration{
			Name:        tmpl.name,
			Description: tmpl.desc,
			Parameters: &tools.Schema{
				Type:       "object",
				Properties: tmpl.props,
			},
		}
	}
	return decls
}

// ---------------------------------------------------------------------------
// Content builders
// ---------------------------------------------------------------------------

var textSamples = []string{
	"The user is requesting a detailed analysis of the quarterly financial report. Please focus on revenue trends, cost centers, and year-over-year growth metrics.",
	"I need to refactor the authentication module to support OAuth2 with PKCE flow. The current implementation only supports basic auth and needs to be extended for mobile clients.",
	"Here is the summary of the meeting: we decided to move forward with the microservices architecture, starting with the payment service and user profile service.",
	"Could you help me debug this Kubernetes deployment? The pods are stuck in CrashLoopBackOff and the logs show a connection refused error when trying to reach the database.",
	"The CI pipeline is failing on the integration test stage. I've narrowed it down to a race condition in the event bus that only manifests under load.",
	"Let's design the API schema for the new notification service. We need endpoints for creating templates, scheduling messages, and tracking delivery status.",
	"I'm reviewing the pull request and I have concerns about the error handling in the repository layer. The current approach swallows context deadlines silently.",
	"Please generate a comprehensive test suite for the order processing workflow. Cover edge cases like partial refunds, duplicate transactions, and timeout scenarios.",
	"The monitoring dashboard shows increased latency in the search service since the last deployment. Can you correlate this with the recent index changes?",
	"We need to migrate the legacy user database to the new schema while maintaining backward compatibility for the transition period.",
}

// makeTextOnlyContents returns n *llm.Content entries alternating "user"/"model"
// roles, each with 1–3 text Parts containing realistic strings.
func makeTextOnlyContents(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		numParts := 1 + (i % 3) // 1, 2, or 3 parts
		parts := make([]*llm.Part, numParts)
		for j := 0; j < numParts; j++ {
			parts[j] = &llm.Part{Text: textSamples[(i+j)%len(textSamples)]}
		}
		contents[i] = &llm.Content{Role: role, Parts: parts}
	}
	return contents
}

// makeMixedContents returns n *llm.Content entries with a mix of Part types:
// one text Part, one FunctionCall, one FunctionResponse, one InlineData,
// and one TransientPart. This exercises all code paths in estimatePartChars
// and estimateMapSizeInternal.
func makeMixedContents(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}

		// Build args with 2–5 entries of varying types.
		argCount := 2 + (i % 4) // 2, 3, 4, or 5
		args := make(map[string]interface{}, argCount)
		argKeys := []string{"query", "path", "limit", "format", "verbose"}
		for k := 0; k < argCount; k++ {
			switch k % 3 {
			case 0:
				args[argKeys[k]] = textSamples[(i+k)%len(textSamples)][:30]
			case 1:
				args[argKeys[k]] = float64(10 + i + k)
			case 2:
				args[argKeys[k]] = k%2 == 0
			}
		}

		// Response map with nested values.
		resp := map[string]interface{}{
			"status":  "ok",
			"count":   float64(42 + i),
			"results": []interface{}{"item-a", "item-b", "item-c"},
			"metadata": map[string]interface{}{
				"elapsed_ms": float64(150 + i),
				"cached":     i%2 == 0,
			},
		}

		contents[i] = &llm.Content{
			Role: role,
			Parts: []*llm.Part{
				{Text: textSamples[i%len(textSamples)]},
				{
					FunctionCall: &llm.FunctionCall{
						ID:   "call-" + string(rune('a'+i%26)),
						Name: "search_files",
						Args: args,
					},
				},
				{
					FunctionResponse: &llm.FunctionResponse{
						ID:       "resp-" + string(rune('a'+i%26)),
						Name:     "search_files",
						Response: resp,
					},
				},
				{
					InlineData: &llm.Blob{
						MIMEType: "image/png",
						Data:     []byte("mock-binary-data-for-benchmark"),
					},
				},
			},
			TransientParts: []*llm.Part{
				{Text: "Transient instruction #" + string(rune('0'+i%10))},
			},
		}
	}
	return contents
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkTokenCounter(b *testing.B) {
	b.Run("Empty", func(b *testing.B) {
		counter := NewHeuristicTokenCounter(nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = counter.Count(nil)
		}
	})

	b.Run("TextOnly/NilRegistry", func(b *testing.B) {
		counter := NewHeuristicTokenCounter(nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			contents := makeTextOnlyContents(50)
			_ = counter.Count(contents)
		}
	})

	b.Run("TextOnly/WithRegistry", func(b *testing.B) {
		reg := &stubRegistry{decls: makeToolDecls(20)}
		counter := NewHeuristicTokenCounter(reg)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			contents := makeTextOnlyContents(50)
			_ = counter.Count(contents)
		}
	})

	b.Run("MixedParts", func(b *testing.B) {
		reg := &stubRegistry{decls: makeToolDecls(20)}
		counter := NewHeuristicTokenCounter(reg)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			contents := makeMixedContents(50)
			_ = counter.Count(contents)
		}
	})
}
