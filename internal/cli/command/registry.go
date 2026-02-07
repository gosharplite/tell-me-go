// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package command

import (
	"fmt"
	"sync"
)

var (
	registry = make(map[string]Factory)
	mu       sync.RWMutex
)

// Register adds a command factory to the registry.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = factory
}

// Get retrieves a command factory from the registry by name.
func Get(name string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("command %q not found", name)
	}
	return factory, nil
}
