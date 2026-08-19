// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Command fakeplur is a stdio MCP server that stands in for the PLUR MCP
// server in the offline two-legged E2E suite (issue #1404 / ADR-068). It
// speaks raw newline-delimited JSON-RPC over stdin/stdout with the Go
// standard library only — it must never import the MCP SDK
// (verify-mcp-sdk-confinement greps the whole repo except
// internal/infrastructure/mcp/).
//
// It implements the six tools the tell-me-go memory integration touches:
// plur_inject_hybrid, plur_learn, plur_capture, plur_learn_batch,
// plur_recall, plur_status. In-memory state is guarded by a mutex and
// persisted to the file named by FAKE_PLUR_STORE (atomic write: temp file +
// rename, after every mutation). Out-of-band pinned engrams are seeded from
// the JSON file named by FAKE_PLUR_SEED — the documented no-pin-tool E2E
// setup step (ADR-068 §13). Ids are deterministic (fakeplur-<counter>);
// no randomness is used.
//
// Like the real server, the fake advertises real inputSchema properties and
// rejects a call missing a required parameter with isError: true. The
// FAKE_PLUR_REJECT env var (comma-separated tool names) additionally makes
// the listed tools return isError: true before any processing — the
// reject-writes-only mode the offline failure-surface E2E leg drives
// (issue #1410 §5).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// requiredParams lists the parameters each tool requires; the fake rejects a
// call missing any of them with isError: true — the same convention the real
// server uses (and the fake's own unknown-tool path).
var requiredParams = map[string][]string{
	"plur_capture":     {"summary"},
	"plur_learn":       {"statement"},
	"plur_learn_batch": {"engrams"},
}

// rejectTools is the env-gated reject set: FAKE_PLUR_REJECT=<comma-separated
// tool names> makes the listed tools return isError: true before any
// processing — reject-writes-only reproduces the shipped "injection healthy,
// writes dead" symptom (issue #1410 §5).
type rejectTools map[string]bool

// engram is one memory record in the fake store.
type engram struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Keywords []string `json:"keywords,omitempty"`
	Pinned   bool     `json:"pinned"`
	Agent    string   `json:"agent,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// episode is one captured plur_capture timeline entry stored by the fake:
// {summary, agent, session_id} — the real @plur-ai/mcp plur_capture shape
// (issue #1410). Not a wire contract for plur_learn_batch (that tool takes
// engrams[]).
type episode struct {
	Summary   string    `json:"summary,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// store is the persisted fake state.
type store struct {
	Engrams  []engram  `json:"engrams"`
	Episodes []episode `json:"episodes"`
	Counter  int       `json:"counter"`
}

// server owns the fake's state and the store file path.
type server struct {
	mu        sync.Mutex // guards store
	store     store
	storePath string
	reject    rejectTools // env-gated reject set; read-only after construction
}

// rpcMessage is a JSON-RPC 2.0 message as received from the client. The id
// is kept raw so numeric and string ids are echoed back verbatim.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response written back to the client.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC protocol-level error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	storePath := os.Getenv("FAKE_PLUR_STORE")
	seedPath := os.Getenv("FAKE_PLUR_SEED")

	s := newServer(storePath, seedPath, parseRejectTools())
	if err := s.run(); err != nil {
		fmt.Fprintf(os.Stderr, "fakeplur: %v\n", err)
		os.Exit(1)
	}
}

// parseRejectTools reads FAKE_PLUR_REJECT — a comma-separated tool name
// list — into a reject set. Missing/empty env yields an empty set (no
// rejection); names are trimmed and blank entries skipped.
func parseRejectTools() rejectTools {
	reject := rejectTools{}
	for _, name := range strings.Split(os.Getenv("FAKE_PLUR_REJECT"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			reject[name] = true
		}
	}
	return reject
}

// newServer loads any existing store, merges the out-of-band pinned seed
// engrams, and ensures the store file exists even when the session performs
// no mutations (the leg (i) "the fake ran" assertion depends on this).
// reject is the FAKE_PLUR_REJECT set; it is stored read-only.
func newServer(storePath, seedPath string, reject rejectTools) *server {
	s := &server{
		store:     store{Engrams: []engram{}, Episodes: []episode{}},
		storePath: storePath,
		reject:    reject,
	}
	if storePath != "" {
		if data, err := os.ReadFile(storePath); err == nil {
			_ = json.Unmarshal(data, &s.store)
		}
		if s.store.Engrams == nil {
			s.store.Engrams = []engram{}
		}
		if s.store.Episodes == nil {
			s.store.Episodes = []episode{}
		}
		// Defensive: never mint an id that already exists in a hand-edited
		// store (the counter is authoritative for ids we mint ourselves).
		if s.store.Counter < len(s.store.Engrams) {
			s.store.Counter = len(s.store.Engrams)
		}
	}
	if seedPath != "" {
		if data, err := os.ReadFile(seedPath); err == nil {
			var seeds []engram
			if json.Unmarshal(data, &seeds) == nil {
				s.mu.Lock()
				known := make(map[string]struct{}, len(s.store.Engrams))
				for _, e := range s.store.Engrams {
					known[e.ID] = struct{}{}
				}
				for _, seed := range seeds {
					if _, dup := known[seed.ID]; dup {
						continue
					}
					s.store.Engrams = append(s.store.Engrams, seed)
					known[seed.ID] = struct{}{}
				}
				s.mu.Unlock()
			}
		}
	}
	if storePath != "" {
		_ = s.persist()
	}
	return s
}

// run reads newline-delimited JSON-RPC messages from stdin until EOF and
// writes single-line responses to stdout, flushing after every write.
func (s *server) run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resp, respond := s.handleLine(line)
		if !respond {
			continue
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// handleLine parses one JSON-RPC message and produces the response (plus
// whether a response is expected). Notifications (no id) never receive a
// response, per JSON-RPC 2.0.
func (s *server) handleLine(line string) (rpcResponse, bool) {
	var msg rpcMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}, true
	}

	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		return rpcResponse{}, false // notification
	}

	switch msg.Method {
	case "initialize":
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "fakeplur", "version": "1.0.0"},
		}}, true
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{}}, true
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{
			"tools": s.toolDefinitions(),
		}}, true
	case "tools/call":
		return s.handleToolCall(msg), true
	default:
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + msg.Method}}, true
	}
}

// toolDefinitions returns the six tool declarations advertised by the fake,
// each carrying a JSON-Schema-shaped inputSchema with the real properties
// and required lists the @plur-ai/mcp server advertises (issue #1410).
func (s *server) toolDefinitions() []map[string]interface{} {
	names := []string{
		"plur_inject_hybrid",
		"plur_learn",
		"plur_capture",
		"plur_learn_batch",
		"plur_recall",
		"plur_status",
	}
	tools := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		tools = append(tools, map[string]interface{}{
			"name":        n,
			"description": "fake PLUR tool for offline E2E",
			"inputSchema": toolSchema(n),
		})
	}
	return tools
}

// toolSchema returns the inputSchema for one tool: {"type": "object"} with
// the properties and required parameters matching the real @plur-ai/mcp
// wire schemas (issue #1410). Unknown names fall back to an empty object.
func toolSchema(name string) map[string]interface{} {
	str := func() map[string]interface{} { return map[string]interface{}{"type": "string"} }
	strArr := func() map[string]interface{} {
		return map[string]interface{}{"type": "array", "items": str()}
	}
	engramObj := func() map[string]interface{} {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"statement": str(),
				"scope":     str(),
				"tags":      strArr(),
			},
			"required": []string{"statement"},
		}
	}

	switch name {
	case "plur_capture":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary":    str(),
				"agent":      str(),
				"session_id": str(),
				"tags":       strArr(),
			},
			"required": []string{"summary"},
		}
	case "plur_learn":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"statement": str(),
				"scope":     str(),
				"tags":      strArr(),
			},
			"required": []string{"statement"},
		}
	case "plur_learn_batch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"engrams": map[string]interface{}{"type": "array", "items": engramObj()},
			},
			"required": []string{"engrams"},
		}
	case "plur_inject_hybrid":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task":   str(),
				"budget": map[string]interface{}{"type": "number"},
				"scope":  str(),
			},
		}
	case "plur_recall":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": str(),
				"task":  str(),
			},
		}
	case "plur_status":
		return map[string]interface{}{"type": "object"}
	default:
		return map[string]interface{}{"type": "object"}
	}
}

// handleToolCall dispatches a tools/call request. Unknown tool names return
// an MCP-level tool error (isError true with an error text), mirroring the
// SDK's toolError convention so the caller sees a non-terminal ToolResult.
func (s *server) handleToolCall(msg rpcMessage) rpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &params)
	}
	var args map[string]interface{}
	if len(params.Arguments) > 0 && string(params.Arguments) != "null" {
		_ = json.Unmarshal(params.Arguments, &args)
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	text, isErr := s.dispatchTool(params.Name, args)
	return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Result: map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
		"isError": isErr,
	}}
}

// dispatchTool routes a tool call to its handler and returns the result
// text plus whether the call was an error. The env-gated reject set and the
// required-param check run before any processing — the same isError
// convention the real server (and the fake's unknown-tool path) uses.
func (s *server) dispatchTool(name string, args map[string]interface{}) (string, bool) {
	if s.reject[name] {
		return "rejected by FAKE_PLUR_REJECT: " + name, true
	}
	for _, param := range requiredParams[name] {
		if v, ok := args[param]; !ok {
			return "missing required parameter: " + param, true
		} else if strVal, isStr := v.(string); isStr && strVal == "" {
			// Present-but-empty required string: the real server rejects
			// empty required values too (issue #1410) — treat as missing.
			return "missing required parameter: " + param, true
		}
	}
	switch name {
	case "plur_inject_hybrid", "plur_recall":
		task := strArg(args, "task")
		if task == "" {
			task = strArg(args, "query")
		}
		return s.recall(task), false
	case "plur_learn":
		return s.learn(args), false
	case "plur_capture":
		return s.capture(args), false
	case "plur_learn_batch":
		return s.learnBatch(args), false
	case "plur_status":
		return s.status(), false
	default:
		return "unknown tool: " + name, true
	}
}

// recall implements the shared inject/recall selection: pinned engrams
// always; non-pinned engrams when any keyword is a case-insensitive
// substring of the task. Pinned engrams come first, then non-pinned, both
// in store insertion order.
func (s *server) recall(task string) string {
	lowerTask := strings.ToLower(task)

	s.mu.Lock()
	var pinned, matched []engram
	for _, e := range s.store.Engrams {
		if e.Pinned {
			pinned = append(pinned, e)
			continue
		}
		if keywordMatches(e.Keywords, lowerTask) {
			matched = append(matched, e)
		}
	}
	selected := append(pinned, matched...)
	s.mu.Unlock()

	if len(selected) == 0 {
		return "No relevant memory."
	}

	var b strings.Builder
	for i, e := range selected {
		if i > 0 {
			b.WriteString("\n\n")
		}
		// The engram_id: line satisfies the injector's documented regex
		// fallback for extracting ids from ToolResult.Text when
		// Metadata["ids"] is absent (internal/agent/memory/injector.go).
		fmt.Fprintf(&b, "engram_id: %s\n%s", e.ID, e.Text)
	}
	return b.String()
}

// learn mints a deterministic fakeplur-<counter> id, stores the statement
// as a non-pinned engram ({statement, scope?, tags} — no agent param on the
// real tool, issue #1410), and persists.
func (s *server) learn(args map[string]interface{}) string {
	statement := strArg(args, "statement")

	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Counter++
	id := fmt.Sprintf("fakeplur-%d", s.store.Counter)
	s.store.Engrams = append(s.store.Engrams, engram{
		ID:       id,
		Text:     statement,
		Keywords: keywordsOf(statement),
		Scope:    strArg(args, "scope"),
		Tags:     strSliceArg(args, "tags"),
	})
	s.persistLocked()
	return "learned " + id
}

// capture appends one episode record to the store and persists. The wire
// shape is {summary, agent, session_id} — the real plur_capture contract
// (issue #1410); text/error/prompt are gone.
func (s *server) capture(args map[string]interface{}) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Episodes = append(s.store.Episodes, episode{
		Summary:   strArg(args, "summary"),
		Agent:     strArg(args, "agent"),
		SessionID: strArg(args, "session_id"),
		Timestamp: time.Now(),
	})
	s.persistLocked()
	return "captured"
}

// learnBatch creates one engram per plur_learn_batch item and persists. The
// real tool takes engrams[] and never creates episodes (issue #1410);
// empty statements are skipped.
func (s *server) learnBatch(args map[string]interface{}) string {
	var items []map[string]interface{}
	if raw, ok := args["engrams"].([]interface{}); ok {
		for _, e := range raw {
			if m, ok := e.(map[string]interface{}); ok {
				items = append(items, m)
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	learned := 0
	for _, m := range items {
		statement := strArg(m, "statement")
		if strings.TrimSpace(statement) == "" {
			continue
		}
		s.store.Counter++
		s.store.Engrams = append(s.store.Engrams, engram{
			ID:       fmt.Sprintf("fakeplur-%d", s.store.Counter),
			Text:     statement,
			Keywords: keywordsOf(statement),
			Scope:    strArg(m, "scope"),
			Tags:     strSliceArg(m, "tags"),
		})
		learned++
	}
	s.persistLocked()
	return fmt.Sprintf("learned %d engrams", learned)
}

// status reports the store's engram/episode counts.
func (s *server) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("fakeplur status: engrams=%d episodes=%d", len(s.store.Engrams), len(s.store.Episodes))
}

// persist writes the store to disk atomically (temp + rename). Callers
// must hold s.mu.
func (s *server) persistLocked() {
	if s.storePath == "" {
		return
	}
	if err := atomicWrite(s.storePath, s.store); err != nil {
		fmt.Fprintf(os.Stderr, "fakeplur: persist: %v\n", err)
	}
}

// persist is the lock-taking wrapper used at startup.
func (s *server) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storePath == "" {
		return nil
	}
	return atomicWrite(s.storePath, s.store)
}

// atomicWrite marshals v to JSON and replaces path via a temp file +
// rename, so a crash never leaves a truncated store.
func atomicWrite(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".fakeplur-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// keywordMatches reports whether any keyword is a case-insensitive
// substring of lowerTask. The caller lowercases task beforehand.
func keywordMatches(keywords []string, lowerTask string) bool {
	for _, k := range keywords {
		if k != "" && strings.Contains(lowerTask, k) {
			return true
		}
	}
	return false
}

// keywordsOf derives the deterministic keyword list from a statement:
// lowercased words split on non-alphanumerics, deduplicated, order
// preserving. Mirrors the PLUR store's documented keyword extraction
// contract for the fake (ADR-068 two-legged E2E).
func keywordsOf(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

// strArg reads a string argument from a decoded JSON object.
func strArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// strSliceArg reads a []string argument from a decoded JSON object,
// accepting []interface{} of strings and skipping non-string/empty entries.
func strSliceArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
