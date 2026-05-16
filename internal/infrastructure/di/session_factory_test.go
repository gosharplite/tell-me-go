// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSessionFactory_ErrorWrappingFormat(t *testing.T) {
	innerErr := errors.New("some security error")
	wrapped := fmt.Errorf("%w: security setup: %w", errInfraInit, innerErr)

	if !errors.Is(wrapped, errInfraInit) {
		t.Error("errors.Is(wrapped, errInfraInit) should be true")
	}
	if !errors.Is(wrapped, innerErr) {
		t.Error("errors.Is(wrapped, innerErr) should be true")
	}

	msg := wrapped.Error()
	if !strings.Contains(msg, "security setup") {
		t.Errorf("error message should contain 'security setup', got: %s", msg)
	}
	if strings.Contains(msg, "paths") {
		t.Errorf("error message should not contain raw paths, got: %s", msg)
	}
}
