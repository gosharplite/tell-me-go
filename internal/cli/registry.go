// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"sync"
)

var (
	cmdRegistry = make(map[string]factory)
	mu          sync.RWMutex
)

// register adds a command factory to the registry.
func register(name string, factory factory) {
	mu.Lock()
	defer mu.Unlock()
	cmdRegistry[name] = factory
}

// get retrieves a command factory from the registry by name.
func get(name string) (factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	factory, ok := cmdRegistry[name]
	if !ok {
		return nil, fmt.Errorf("command %q not found", name)
	}
	return factory, nil
}
