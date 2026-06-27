// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package plugin provides the self-registering plugin interface and global
// registry for integration tool discovery.
//
// Plugins implement the Plugin interface and call Register during init() or
// at application startup. The registry enforces unique names and provides
// snapshot access to all registered plugins for batch initialization.
package plugin

import (
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Plugin describes an auto-registering integration plugin. Implementations
// provide a unique name and a Register method that wires the plugin's tools
// into the tool Registry using the provided PluginDependencies.
type Plugin interface {
	// Name returns a unique identifier for this plugin, used for
	// duplicate detection in the global registry.
	Name() string

	// Register wires the plugin's tools into r. Implementations must
	// use the provided dependencies to construct tool handlers.
	//
	// Register is called exactly once at application startup. An error
	// triggers a fail-fast shutdown of the integration subsystem.
	Register(r tools.Registry, deps PluginDependencies) error
}

// PluginDependencies provides the injectable dependencies needed by
// plugin implementations during registration.
//
// All fields are optional at the type level; plugins should handle
// nil values gracefully by skipping optional functionality or
// substituting safe defaults.
type PluginDependencies struct {
	// FileSystem provides filesystem operations for tools that
	// read or write local files (e.g., media tools, workspace).
	FileSystem persistence.FileSystem

	// SecurityMgr provides the security manager for path validation,
	// command authorization, and user consent.
	SecurityMgr domain_security.Manager

	// LLMClient provides the LLM client for tools that need AI
	// capabilities (e.g., image generation, summarization).
	LLMClient llm.LLMClient

	// HTTPClient provides an HTTP client for tools that make HTTP
	// requests (e.g., network tools, Confluence, Jira, ADO).
	//
	// This is separate from LLMClient because integration plugins
	// call external APIs and do not need the full LLM interface.
	HTTPClient tools.HTTPClient

	// AssetsDir is the path to the assets directory for serving
	// static resources (e.g., media tool assets).
	AssetsDir string
}

var (
	mu      sync.Mutex
	plugins []Plugin
)

// Register adds p to the global plugin registry. It returns an error if
// another plugin with the same Name() is already registered. Register is
// safe for concurrent use.
//
// A nil plugin is rejected with an error.
func Register(p Plugin) error {
	if p == nil {
		return fmt.Errorf("plugin.Register: nil plugin")
	}

	mu.Lock()
	defer mu.Unlock()

	name := p.Name()
	for _, existing := range plugins {
		if existing.Name() == name {
			return fmt.Errorf("plugin.Register: duplicate plugin name %q", name)
		}
	}

	plugins = append(plugins, p)
	return nil
}

// All returns a copy of all registered plugins. The returned slice is
// safe to mutate without affecting the internal registry.
//
// All is safe for concurrent use.
func All() []Plugin {
	mu.Lock()
	defer mu.Unlock()

	out := make([]Plugin, len(plugins))
	copy(out, plugins)
	return out
}

// Reset clears all registered plugins. This is intended for test cleanup
// only and must not be used in production code paths.
//
//nolint:deadcode
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	plugins = nil
}
