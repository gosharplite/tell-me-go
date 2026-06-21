// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type chatServiceDefaultTest struct {
	name string
	fn   func(*testing.T, *MockChatService)
}

var chatServiceDefaultTests = []chatServiceDefaultTest{
	{
		name: "ProcessMessage",
		fn: func(t *testing.T, mcs *MockChatService) {
			err := mcs.ProcessMessage(
				context.Background(), nil, agent.ChatCommand{}, nil)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetLastUserMessage",
		fn: func(t *testing.T, mcs *MockChatService) {
			msg, n, err := mcs.GetLastUserMessage(
				context.Background(), nil)
			if msg != "" {
				t.Errorf("expected empty string, got %q", msg)
			}
			if n != 0 {
				t.Errorf("expected 0, got %d", n)
			}
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "BrowseHistory",
		fn: func(t *testing.T, mcs *MockChatService) {
			err := mcs.BrowseHistory(
				context.Background(), nil, nil)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "GetToolNames",
		fn: func(t *testing.T, mcs *MockChatService) {
			names, err := mcs.GetToolNames(
				context.Background(), nil)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
			want := []string{"test_tool"}
			if !reflect.DeepEqual(names, want) {
				t.Errorf("names = %v, want %v", names, want)
			}
		},
	},
	{
		name: "StreamTurnsLog",
		fn: func(t *testing.T, mcs *MockChatService) {
			err := mcs.StreamTurnsLog(
				context.Background(), nil, nil)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
	{
		name: "RunDiagnostics",
		fn: func(t *testing.T, mcs *MockChatService) {
			err := mcs.RunDiagnostics(
				context.Background(), nil, "", false)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		},
	},
}

func testChatServiceSnapshotCallTracking(t *testing.T) {
	mcs := &MockChatService{}
	_ = mcs.ProcessMessage(
		context.Background(), nil, agent.ChatCommand{}, nil)
	_, _, _ = mcs.GetLastUserMessage(
		context.Background(), nil)
	_ = mcs.BrowseHistory(
		context.Background(), nil, nil)

	snap := mcs.Snapshot()

	if snap.ProcessMessage != 1 {
		t.Errorf("ProcessMessage = %d, want 1",
			snap.ProcessMessage)
	}
	if snap.GetLastUserMessage != 1 {
		t.Errorf("GetLastUserMessage = %d, want 1",
			snap.GetLastUserMessage)
	}
	if snap.BrowseHistory != 1 {
		t.Errorf("BrowseHistory = %d, want 1",
			snap.BrowseHistory)
	}

	want := []string{"ProcessMessage", "GetLastUserMessage", "BrowseHistory"}
	if !reflect.DeepEqual(snap.Methods, want) {
		t.Errorf("Methods = %v, want %v", snap.Methods, want)
	}
}

func TestMockChatService_DefaultReturns(t *testing.T) {
	t.Parallel()

	for _, tt := range chatServiceDefaultTests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, &MockChatService{})
		})
	}

	t.Run("Snapshot_call_tracking", testChatServiceSnapshotCallTracking)
}

func TestMockChatService_GetLastUserMessage_NonNilFunc(t *testing.T) {
	t.Parallel()

	mcs := &MockChatService{
		GetLastUserMessageFunc: func(ctx context.Context, hManager ports.HistoryManager) (string, int, error) {
			return "hello", 3, nil
		},
	}

	msg, n, err := mcs.GetLastUserMessage(context.Background(), nil)

	if msg != "hello" {
		t.Errorf("expected message %q, got %q", "hello", msg)
	}
	if n != 3 {
		t.Errorf("expected rollback count %d, got %d", 3, n)
	}
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	snap := mcs.Snapshot()
	if snap.GetLastUserMessage != 1 {
		t.Errorf("GetLastUserMessage count = %d, want 1",
			snap.GetLastUserMessage)
	}
}
