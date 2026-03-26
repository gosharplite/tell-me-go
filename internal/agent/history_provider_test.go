// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
)

type mockArchiveReader struct {
	readPageFunc func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error)
}

func (m *mockArchiveReader) ReadPage(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	return m.readPageFunc(ctx, limit, offset)
}

type mockHistoryManager struct {
	ports.HistoryManager
	getWindowFunc func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)
}

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return m.getWindowFunc(ctx, startIdx, endIdx)
}

func TestUnifiedProvider_GetHistoryStream_Filtering(t *testing.T) {
	ctx := context.Background()

	// 1. Mock Active History (with an auto-summary message)
	active := &mockHistoryManager{
		getWindowFunc: func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
			return []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello world"}}},
				{Role: "system", Parts: []*llm.Part{{Text: "System Auto-Summary: blablabla"}}},
				{Role: "assistant", Parts: []*llm.Part{{Text: "Understood"}}},
			}, nil
		},
	}

	// 2. Mock Archive Reader (empty for this test)
	archive := &mockArchiveReader{
		readPageFunc: func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
			return nil, offset, nil
		},
	}

	provider := history.NewUnifiedProvider(archive, active)

	t.Run("it filters out auto-summary messages from active history", func(t *testing.T) {
		dtos, nextCursor, err := provider.GetHistoryStream(ctx, 10, "")
		if err != nil {
			t.Fatalf("failed to get history stream: %v", err)
		}

		if len(dtos) != 2 {
			t.Fatalf("expected 2 DTOs (auto-summary should be filtered), got %d", len(dtos))
		}

		for _, dto := range dtos {
			if strings.Contains(dto.ContentPreview, "System Auto-Summary:") {
				t.Errorf("found filtered content in DTO: %s", dto.ContentPreview)
			}
		}

		if nextCursor != "archive:0" {
			t.Errorf("expected archive:0 cursor, got %s", nextCursor)
		}
	})

	t.Run("it paginates archive when using archive: prefix", func(t *testing.T) {
		archiveCalled := false
		archive.readPageFunc = func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
			if offset != 123 {
				t.Errorf("expected offset 123, got %d", offset)
			}
			archiveCalled = true
			return []ports.HistoryViewDTO{{ContentPreview: "Archived content"}}, 456, nil
		}

		dtos, nextCursor, err := provider.GetHistoryStream(ctx, 10, "archive:123")
		if err != nil {
			t.Fatalf("failed to get archive page: %v", err)
		}

		if !archiveCalled {
			t.Error("archive reader was not called")
		}

		if len(dtos) != 1 || dtos[0].ContentPreview != "Archived content" {
			t.Errorf("unexpected archive content: %+v", dtos)
		}

		if nextCursor != "archive:456" {
			t.Errorf("expected archive:456 cursor, got %s", nextCursor)
		}
	})
}
