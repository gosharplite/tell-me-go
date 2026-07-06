// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"testing"
	"time"
)

func TestSafePath(t *testing.T) {
	sp := SafePath{
		Path:         "/home/user/project",
		Mode:         SafePathReadWrite,
		AuthorizedAt: time.Now(),
	}

	if sp.Path != "/home/user/project" {
		t.Errorf("Path = %q, want %q", sp.Path, "/home/user/project")
	}
	if sp.Mode != SafePathReadWrite {
		t.Errorf("Mode = %q, want %q", sp.Mode, SafePathReadWrite)
	}
}

func TestSafePathMode_Constants(t *testing.T) {
	if SafePathReadWrite != "readwrite" {
		t.Errorf("SafePathReadWrite = %q, want %q", SafePathReadWrite, "readwrite")
	}
	if SafePathRead != "read" {
		t.Errorf("SafePathRead = %q, want %q", SafePathRead, "read")
	}
}
