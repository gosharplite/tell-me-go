// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"sync"
)

var (
	cmdRegistry = make(map[string]Factory)
	mu          sync.RWMutex
)

// Register adds a command factory to the registry.
func Register(name string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	cmdRegistry[name] = factory
}

// Get retrieves a command factory from the registry by name.
func Get(name string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := cmdRegistry[name]
	if !ok {
		return nil, fmt.Errorf("command %q not found", name)
	}
	return factory, nil
}
