// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockHistoryManager implements ports.HistoryManager for testing.
type MockHistoryManager struct {
	ports.HistoryManager
	GetWindowFunc func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)
}

func (m *MockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return m.GetWindowFunc(ctx, startIdx, endIdx)
}

// MockArchiveReader implements ports.ArchiveReader for testing.
type MockArchiveReader struct {
	ports.ArchiveReader
	ReadPageFunc func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error)
}

func (m *MockArchiveReader) ReadPage(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	return m.ReadPageFunc(ctx, limit, offset)
}

func TestUnifiedProvider_GetHistoryStream(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		cursor     string
		activeHist []*llm.Content
		archived   []ports.HistoryViewDTO
		nextOffset int64
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
			wantCursor: "archive:0",
		},
		{
			name:   "Archive Read - archive cursor",
			limit:  5,
			cursor: "archive:5",
			archived: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			nextOffset: 10,
			wantDTOs: []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "Archived msg", IsArchived: true},
			},
			wantCursor: "archive:10",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockActive := &MockHistoryManager{
				GetWindowFunc: func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
					return tt.activeHist, nil
				},
			}
			mockArchive := &MockArchiveReader{
				ReadPageFunc: func(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
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
	mockActive := &MockHistoryManager{}
	mockArchive := &MockArchiveReader{}
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

	dto := p.toDTO(c, true)

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
