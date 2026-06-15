// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"
	"reflect"
	"testing"
)

// assertNil is a generic nil-check helper for table-driven tests.
// It uses the any(v) != nil conversion to correctly detect nil
// interface values wrapping nil concrete types.
func assertNil[T any](t *testing.T, label string, v T) {
	t.Helper()
	if any(v) != nil {
		t.Errorf("expected nil %s", label)
	}
}

type defaultReturnTest struct {
	name string
	fn   func(*testing.T, *MockBootstrapper)
}

var defaultReturnTests = []defaultReturnTest{
	{
		name: "BuildSessionDependencies",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			chatter, hm, closeFn, err := mb.BuildSessionDependencies(
				context.Background(), nil, "", false, nil)
			if chatter != nil {
				t.Error("expected nil ChatterComposer")
			}
			if hm != nil {
				t.Error("expected nil HistoryManager")
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
			if closeFn == nil {
				t.Fatal("expected non-nil close func")
			}
			if cerr := closeFn(context.Background()); cerr != nil {
				t.Errorf("close func should return nil, got %v", cerr)
			}
		},
	},
	{
		name: "FinalizeSession",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			err := mb.FinalizeSession(context.Background(), nil, nil, nil)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetHistoryManager",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			hm, err := mb.GetHistoryManager(context.Background(), nil)
			if hm != nil {
				t.Error("expected nil HistoryManager")
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetUnifiedHistoryProvider",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			p, err := mb.GetUnifiedHistoryProvider(context.Background(), nil, nil)
			if p != nil {
				t.Error("expected nil UnifiedHistoryProvider")
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetSuggestionService",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			s, err := mb.GetSuggestionService(context.Background(), nil)
			if s != nil {
				t.Error("expected nil SuggestionService")
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetAgentFactory",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			if f := mb.GetAgentFactory(); f != nil {
				t.Error("expected nil ChatterFactory")
			}
		},
	},
	{
		name: "GetUIRenderer",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			assertNil(t, "UIRenderer", mb.GetUIRenderer())
		},
	},
	{
		name: "GetHistoryRenderer",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			assertNil(t, "HistoryRenderer", mb.GetHistoryRenderer())
		},
	},
	{
		name: "GetHistoryBrowser",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			assertNil(t, "HistoryBrowser", mb.GetHistoryBrowser())
		},
	},
	{
		name: "GetChatService",
		fn: func(t *testing.T, mb *MockBootstrapper) {
			assertNil(t, "ChatService", mb.GetChatService())
		},
	},
}

func testSnapshotCallTracking(t *testing.T) {
	mb := &MockBootstrapper{}
	_, _, _, _ = mb.BuildSessionDependencies(
		context.Background(), nil, "", false, nil)
	_ = mb.FinalizeSession(context.Background(), nil, nil, nil)

	snap := mb.Snapshot()

	if snap.BuildSessionDependencies != 1 {
		t.Errorf("BuildSessionDependencies = %d, want 1",
			snap.BuildSessionDependencies)
	}
	if snap.FinalizeSession != 1 {
		t.Errorf("FinalizeSession = %d, want 1",
			snap.FinalizeSession)
	}

	want := []string{"BuildSessionDependencies", "FinalizeSession"}
	if !reflect.DeepEqual(snap.Methods, want) {
		t.Errorf("Methods = %v, want %v", snap.Methods, want)
	}
}

func TestMockBootstrapper_DefaultReturns(t *testing.T) {
	t.Parallel()

	for _, tt := range defaultReturnTests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, &MockBootstrapper{})
		})
	}

	t.Run("Snapshot_call_tracking", testSnapshotCallTracking)
}
