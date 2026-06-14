// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"
	"reflect"
	"testing"
)

func TestMockBootstrapper_DefaultReturns(t *testing.T) {
	t.Parallel()

	t.Run("BuildSessionDependencies", func(t *testing.T) {
		mb := &MockBootstrapper{}
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
	})

	t.Run("FinalizeSession", func(t *testing.T) {
		mb := &MockBootstrapper{}
		err := mb.FinalizeSession(context.Background(), nil, nil, nil)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("GetHistoryManager", func(t *testing.T) {
		mb := &MockBootstrapper{}
		hm, err := mb.GetHistoryManager(context.Background(), nil)
		if hm != nil {
			t.Error("expected nil HistoryManager")
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("GetUnifiedHistoryProvider", func(t *testing.T) {
		mb := &MockBootstrapper{}
		p, err := mb.GetUnifiedHistoryProvider(context.Background(), nil, nil)
		if p != nil {
			t.Error("expected nil UnifiedHistoryProvider")
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("GetSuggestionService", func(t *testing.T) {
		mb := &MockBootstrapper{}
		s, err := mb.GetSuggestionService(context.Background(), nil)
		if s != nil {
			t.Error("expected nil SuggestionService")
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("GetAgentFactory", func(t *testing.T) {
		mb := &MockBootstrapper{}
		f := mb.GetAgentFactory()
		if f != nil {
			t.Error("expected nil ChatterFactory")
		}
	})

	t.Run("GetUIRenderer", func(t *testing.T) {
		mb := &MockBootstrapper{}
		r := mb.GetUIRenderer()
		if r != nil {
			t.Error("expected nil UIRenderer")
		}
	})

	t.Run("GetHistoryRenderer", func(t *testing.T) {
		mb := &MockBootstrapper{}
		r := mb.GetHistoryRenderer()
		if r != nil {
			t.Error("expected nil HistoryRenderer")
		}
	})

	t.Run("GetHistoryBrowser", func(t *testing.T) {
		mb := &MockBootstrapper{}
		b := mb.GetHistoryBrowser()
		if b != nil {
			t.Error("expected nil HistoryBrowser")
		}
	})

	t.Run("GetChatService", func(t *testing.T) {
		mb := &MockBootstrapper{}
		s := mb.GetChatService()
		if s != nil {
			t.Error("expected nil ChatService")
		}
	})

	t.Run("Snapshot_call_tracking", func(t *testing.T) {
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
	})
}
