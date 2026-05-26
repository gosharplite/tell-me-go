// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

// generateTasks creates n realistic Task fixtures for benchmarking.
func generateTasks(n int) []ports.Task {
	tasks := make([]ports.Task, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		tasks[i] = ports.Task{
			ID:        int64(i + 1),
			Content:   fmt.Sprintf("Task number %d with some descriptive text for realistic JSON size", i+1),
			Status:    "pending",
			CreatedAt: now,
		}
	}
	return tasks
}

// marshalTasksToJSONL marshals a slice of tasks into JSONL format (one JSON object per line).
func marshalTasksToJSONL(tasks []ports.Task) string {
	var sb strings.Builder
	for _, t := range tasks {
		data, err := json.Marshal(t)
		if err != nil {
			panic(fmt.Sprintf("marshalTasksToJSONL: failed to marshal task: %v", err))
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// BenchmarkParseJSONLTasks measures the performance of parseJSONLTasks across
// realistic input sizes with zero filesystem I/O.
func BenchmarkParseJSONLTasks(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sizes := []int{100, 1000, 10000, 50000}

	for _, size := range sizes {
		tasks := generateTasks(size)
		fixture := marshalTasksToJSONL(tasks)

		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = parseJSONLTasks(fixture, "bench.jsonl", logger)
			}
		})
	}
}

// BenchmarkTaskRepositoryReadAll measures end-to-end read performance of
// taskRepository.ReadAll with real disk I/O, covering both JSONL and JSON
// array formats.
func BenchmarkTaskRepositoryReadAll(b *testing.B) {
	sizes := []int{1000, 10000, 50000}
	formats := []struct {
		name    string
		marshal func([]ports.Task) []byte
	}{
		{
			name: "jsonl",
			marshal: func(tasks []ports.Task) []byte {
				return []byte(marshalTasksToJSONL(tasks))
			},
		},
		{
			name: "json_array",
			marshal: func(tasks []ports.Task) []byte {
				data, err := json.Marshal(tasks)
				if err != nil {
					panic(fmt.Sprintf("json.Marshal tasks: %v", err))
				}
				return data
			},
		},
	}

	for _, format := range formats {
		b.Run("format="+format.name, func(b *testing.B) {
			for _, size := range sizes {
				b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
					ctx := context.Background()
					tmpDir := b.TempDir()
					fs := persistencetest.NewPlainOSFileSystem()
					filePath := filepath.Join(tmpDir, "tasks.json")

					tasks := generateTasks(size)
					content := format.marshal(tasks)

					if err := fs.WriteFile(ctx, filePath, content, 0644); err != nil {
						b.Fatalf("WriteFile: %v", err)
					}

					repo := newTaskRepository(fs, filePath, slog.New(slog.NewTextHandler(io.Discard, nil)))

					b.ResetTimer()
					b.ReportAllocs()

					for i := 0; i < b.N; i++ {
						_, _ = repo.ReadAll(ctx)
					}
				})
			}
		})
	}
}
