package persistence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDebugTaskMigration(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()
	tasksFile := filepath.Join(tempDir, "tasks.json")

	// Create a corrupted tasks.json with an 'i' at the start of a line
	content := `{"id": 1, "content": "Valid Task"}
invalid json line starting with i
{"id": 2, "content": "Another Task"}
`
	if err := os.WriteFile(tasksFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Now try to initialize the state, which should trigger migration
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatalf("Failed to initialize session state: %v", err)
	}
	defer func() {
		_ = state.Close()
	}()

	tasks := state.GetTasks().ListTasks("", 0, 0)

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d. Corrupted line might not have been skipped correctly", len(tasks))
	}
}
