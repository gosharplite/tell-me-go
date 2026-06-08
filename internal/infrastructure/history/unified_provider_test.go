// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// mockHistoryManager implements ports.HistoryManager for testing.
type mockHistoryManager struct {
	ports.HistoryManager
	GetWindowFunc func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)
	GetWindowErr  error
}

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	if m.GetWindowErr != nil {
		return nil, m.GetWindowErr
	}
	return m.GetWindowFunc(ctx, startIdx, endIdx)
}

// mockArchiveReader implements ports.ArchiveReader for testing.
type mockArchiveReader struct {
	ports.ArchiveReader
	ReadPageFunc     func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error)
	ReadPreviousFunc func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error)
	ReadPreviousErr  error
}

func (m *mockArchiveReader) ReadPage(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	return m.ReadPageFunc(ctx, limit, offset)
}

func (m *mockArchiveReader) ReadPrevious(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	if m.ReadPreviousErr != nil {
		return nil, 0, m.ReadPreviousErr
	}
	return m.ReadPreviousFunc(ctx, limit, offset)
}

func TestUnifiedProvider_GetHistoryStream(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		cursor     string
		activeHist []*llm.Content
		activeErr  error
		archived   []ports.HistoryViewDTO
		nextOffset int64
		archiveErr error
		wantErr    bool
		wantDTOs   []ports.HistoryViewDTO
		wantCursor string
	}{
		{
			name:   "Active Memory Read - empty cursor",
			limit:  10,
			cursor: "",
			activeHist: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "Hello"},
					},
				},
				{
					Role: "system",
					Parts: []*llm.Part{
						{Text: "System Auto-Summary: this should be skipped"},
					},
				},
				{
					Role: "model",
					Parts: []*llm.Part{
						{Text: "Response"},
						{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("base64data")}},
					},
				},
			},
			wantDTOs: []ports.HistoryViewDTO{
				{
					Role:           "user",
					ContentPreview: "Hello",
					IsArchived:     false,
					OriginalIndex:  0,
					ToolCalls:      nil,
				},
				{
					Role:           "model",
					ContentPreview: "Response [Image Attached]",
					IsArchived:     false,
					OriginalIndex:  2,
					ToolCalls:      nil,
				},
			},
			wantCursor: "archive:-1",
		},
		{
			name:   "Archive Read - archive cursor",
			limit:  5,
			cursor: "archive:100",
			archived: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			nextOffset: 50,
			wantDTOs: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			wantCursor: "archive:50",
		},
		{
			name:    "Invalid cursor format",
			cursor:  "invalid:123",
			wantErr: true,
		},
		{
			name:    "Invalid archive offset",
			cursor:  "archive:abc",
			wantErr: true,
		},
		// Gap 11: GetWindow error in fetchActiveHistory
		{
			name:      "GetWindow error",
			limit:     10,
			cursor:    "",
			activeErr: errors.New("active history unavailable"),
			wantErr:   true,
		},
		// Gap 12: ReadPrevious error in fetchArchiveHistory
		{
			name:       "ReadPrevious error",
			limit:      10,
			cursor:     "archive:100",
			archiveErr: errors.New("archive read failed"),
			wantErr:    true,
		},
		// Gap 13: EOF edge cases
		{
			name:       "archive cursor at zero (EOF)",
			limit:      10,
			cursor:     "archive:0",
			wantCursor: "EOF",
			wantDTOs:   nil,
		},
		{
			name:       "empty active history returns archive:-1 cursor",
			limit:      10,
			cursor:     "",
			activeHist: []*llm.Content{},
			wantCursor: "archive:-1",
			wantDTOs:   nil,
		},
		{
			name:   "nil content in active history",
			limit:  10,
			cursor: "",
			activeHist: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "First"},
					},
				},
				nil, // Should be skipped by fetchActiveHistory nil check
				{
					Role: "model",
					Parts: []*llm.Part{
						{Text: "Second"},
					},
				},
			},
			wantDTOs: []ports.HistoryViewDTO{
				{
					Role:           "user",
					ContentPreview: "First",
					IsArchived:     false,
					OriginalIndex:  0,
					ToolCalls:      nil,
				},
				{
					Role:           "model",
					ContentPreview: "Second",
					IsArchived:     false,
					OriginalIndex:  2,
					ToolCalls:      nil,
				},
			},
			wantCursor: "archive:-1",
		},
		{
			name:   "archive read with zero nextOffset returns EOF cursor",
			limit:  5,
			cursor: "archive:100",
			archived: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			nextOffset: 0,
			wantDTOs: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			wantCursor: "EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActive := &mockHistoryManager{
				GetWindowErr: tt.activeErr,
				GetWindowFunc: func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
					return tt.activeHist, nil
				},
			}
			mockArchive := &mockArchiveReader{
				ReadPreviousErr: tt.archiveErr,
				ReadPreviousFunc: func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
					return tt.archived, tt.nextOffset, nil
				},
			}

			provider := NewUnifiedProvider(mockArchive, mockActive)
			gotDTOs, gotCursor, err := provider.GetHistoryStream(context.Background(), tt.limit, tt.cursor)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetHistoryStream() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if gotCursor != tt.wantCursor {
					t.Errorf("GetHistoryStream() gotCursor = %v, want %v", gotCursor, tt.wantCursor)
				}
				if !reflect.DeepEqual(gotDTOs, tt.wantDTOs) {
					t.Errorf("GetHistoryStream() gotDTOs = %+v, want %+v", gotDTOs, tt.wantDTOs)
				}
			}
		})
	}
}

func TestUnifiedProvider_ToDTO_ExtraCases(t *testing.T) {
	// Testing parts like thoughts and tool calls which weren't fully covered in the main test.
	mockActive := &mockHistoryManager{}
	mockArchive := &mockArchiveReader{}
	p := NewUnifiedProvider(mockArchive, mockActive)

	c := &llm.Content{
		Role:   "model",
		Pinned: true,
		Parts: []*llm.Part{
			{IsThought: true, Text: "Thinking..."},
			{Text: "Done."},
			{FunctionCall: &llm.FunctionCall{Name: "get_weather"}},
			{FunctionResponse: &llm.FunctionResponse{Name: "get_weather"}},
			{AssetID: "asset-123"},
			nil, // Should be handled gracefully
		},
	}

	dto := p.(*unifiedProvider).toDTO(c, true)

	if dto.Role != "model" {
		t.Errorf("expected role model, got %s", dto.Role)
	}
	if !dto.IsPinned {
		t.Error("expected IsPinned to be true")
	}
	if !dto.IsArchived {
		t.Error("expected IsArchived to be true")
	}
	if dto.ThoughtProcess != "Thinking..." {
		t.Errorf("expected thought 'Thinking...', got '%s'", dto.ThoughtProcess)
	}
	expectedPreview := "Done. [Attached Asset: asset-123]"
	if dto.ContentPreview != expectedPreview {
		t.Errorf("expected preview '%s', got '%s'", expectedPreview, dto.ContentPreview)
	}
	if len(dto.ToolCalls) != 2 || dto.ToolCalls[0] != "get_weather" || dto.ToolCalls[1] != "get_weather" {
		t.Errorf("expected tool calls [get_weather, get_weather], got %v", dto.ToolCalls)
	}
}
