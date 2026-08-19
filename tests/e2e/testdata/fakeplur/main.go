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

// engram is one memory record in the fake store.
type engram struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Keywords []string `json:"keywords,omitempty"`
	Pinned   bool     `json:"pinned"`
	Agent    string   `json:"agent,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

// episode is one captured learning episode (mirrors the plur_learn_batch
// wire contract defined by internal/agent/memory/buffer.go episode).
type episode struct {
	Agent     string    `json:"agent,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Text      string    `json:"text,omitempty"`
	Error     string    `json:"error,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
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

	s := newServer(storePath, seedPath)
	if err := s.run(); err != nil {
		fmt.Fprintf(os.Stderr, "fakeplur: %v\n", err)
		os.Exit(1)
	}
}

// newServer loads any existing store, merges the out-of-band pinned seed
// engrams, and ensures the store file exists even when the session performs
// no mutations (the leg (i) "the fake ran" assertion depends on this).
func newServer(storePath, seedPath string) *server {
	s := &server{
		store:     store{Engrams: []engram{}, Episodes: []episode{}},
		storePath: storePath,
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

// toolDefinitions returns the six tool declarations advertised by the fake.
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
			"inputSchema": map[string]interface{}{"type": "object"},
		})
	}
	return tools
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
// text plus whether the call was an error.
func (s *server) dispatchTool(name string, args map[string]interface{}) (string, bool) {
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
// as a non-pinned engram, and persists.
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
		Agent:    strArg(args, "agent"),
		Scope:    strArg(args, "scope"),
	})
	s.persistLocked()
	return "learned " + id
}

// capture appends one episode record to the store and persists.
func (s *server) capture(args map[string]interface{}) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Episodes = append(s.store.Episodes, episode{
		Agent:     strArg(args, "agent"),
		SessionID: strArg(args, "session_id"),
		Text:      strArg(args, "text"),
		Error:     strArg(args, "error"),
		Prompt:    strArg(args, "prompt"),
		Timestamp: time.Now(),
	})
	s.persistLocked()
	return "captured"
}

// learnBatch appends each episode to the store's episodes section and
// learns each non-empty episode as an engram too (so a later recall can
// find it), then persists.
func (s *server) learnBatch(args map[string]interface{}) string {
	var episodes []map[string]interface{}
	if raw, ok := args["episodes"].([]interface{}); ok {
		for _, e := range raw {
			if m, ok := e.(map[string]interface{}); ok {
				episodes = append(episodes, m)
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range episodes {
		ep := episode{
			Agent:     strArg(m, "agent"),
			SessionID: strArg(m, "session_id"),
			Text:      strArg(m, "text"),
			Error:     strArg(m, "error"),
			Prompt:    strArg(m, "prompt"),
			Timestamp: time.Now(),
		}
		s.store.Episodes = append(s.store.Episodes, ep)
		if strings.TrimSpace(ep.Text) != "" {
			s.store.Counter++
			s.store.Engrams = append(s.store.Engrams, engram{
				ID:       fmt.Sprintf("fakeplur-%d", s.store.Counter),
				Text:     ep.Text,
				Keywords: keywordsOf(ep.Text),
				Agent:    ep.Agent,
			})
		}
	}
	s.persistLocked()
	return fmt.Sprintf("learned %d episodes", len(episodes))
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
