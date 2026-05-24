// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testfixtures

import (
	"testing"
)

func TestSpyLogger_RecordsAllLevels(t *testing.T) {
	t.Parallel()

	sl := &SpyLogger{}
	sl.Error("err1")
	sl.Error("err2")
	sl.Warn("warn1")
	sl.Info("info1")
	sl.Debug("dbg1")
	sl.Debug("dbg2")

	if len(sl.GetErrors()) != 2 {
		t.Errorf("Errors = %d; want 2", len(sl.GetErrors()))
	}
	if len(sl.GetWarns()) != 1 {
		t.Errorf("Warns = %d; want 1", len(sl.GetWarns()))
	}
	if len(sl.GetInfos()) != 1 {
		t.Errorf("Infos = %d; want 1", len(sl.GetInfos()))
	}
	if len(sl.GetDebugs()) != 2 {
		t.Errorf("Debugs = %d; want 2", len(sl.GetDebugs()))
	}
}

func TestSpyLogger_CalledWith(t *testing.T) {
	t.Parallel()

	sl := &SpyLogger{}
	sl.Error("unexpected failure")
	sl.Warn("deprecated config")
	sl.Debug("cache hit")

	tests := []struct {
		level string
		msg   string
		want  bool
	}{
		{"Error", "unexpected failure", true},
		{"Error", "nonexistent", false},
		{"Warn", "deprecated config", true},
		{"Warn", "nonexistent", false},
		{"Info", "anything", false},
		{"Debug", "cache hit", true},
		{"Debug", "cache miss", false},
		{"Bogus", "anything", false},
	}

	for _, tt := range tests {
		got := sl.CalledWith(tt.level, tt.msg)
		if got != tt.want {
			t.Errorf("CalledWith(%q, %q) = %v; want %v", tt.level, tt.msg, got, tt.want)
		}
	}
}

func TestSpyLogger_Reset(t *testing.T) {
	t.Parallel()

	sl := &SpyLogger{}
	sl.Error("e")
	sl.Warn("w")
	sl.Info("i")
	sl.Debug("d")

	sl.Reset()

	if len(sl.GetErrors()) != 0 || len(sl.GetWarns()) != 0 || len(sl.GetInfos()) != 0 || len(sl.GetDebugs()) != 0 {
		t.Error("Reset() did not clear all slices")
	}
}

func TestSpyLogger_ZeroValueReady(t *testing.T) {
	t.Parallel()

	var sl SpyLogger
	sl.Debug("hello")
	if !sl.CalledWith("Debug", "hello") {
		t.Error("zero-value SpyLogger should work without initialization")
	}
}
