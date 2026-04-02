package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestGetSessionEvents(t *testing.T) {
	// Set up an in-memory SQLite database
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()

	// Create events table
	_, err = db.Exec(`CREATE TABLE events (
		id TEXT PRIMARY KEY,
		payload TEXT,
		created_at TEXT
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert test data
	now := time.Now().UTC()
	events := []Event{
		{ID: "event-1", Payload: `{"status": "started"}`, CreatedAt: now},
		{ID: "event-2", Payload: `{"status": "running"}`, CreatedAt: now.Add(time.Second)},
		{ID: "event-3", Payload: `{"status": "completed"}`, CreatedAt: now.Add(2 * time.Second)},
	}

	for _, e := range events {
		_, err := db.Exec("INSERT INTO events (id, payload, created_at) VALUES (?, ?, ?)",
			e.ID, e.Payload, e.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("Failed to insert event %s: %v", e.ID, err)
		}
	}

	store := NewEventStore(db)

	// Test 1: Fetch multiple events (Batch Query)
	t.Run("Fetch multiple IDs", func(t *testing.T) {
		ctx := context.Background()
		ids := []string{"event-1", "event-3"}
		fetched, err := store.GetSessionEvents(ctx, ids)
		if err != nil {
			t.Fatalf("GetSessionEvents failed: %v", err)
		}

		if len(fetched) != 2 {
			t.Fatalf("Expected 2 events, got %d", len(fetched))
		}

		if fetched[0].ID != "event-1" && fetched[1].ID != "event-1" {
			t.Errorf("Expected event-1 to be fetched")
		}
		if fetched[0].ID != "event-3" && fetched[1].ID != "event-3" {
			t.Errorf("Expected event-3 to be fetched")
		}
	})

	// Test 2: Fetch empty slice
	t.Run("Fetch empty slice", func(t *testing.T) {
		ctx := context.Background()
		fetched, err := store.GetSessionEvents(ctx, []string{})
		if err != nil {
			t.Fatalf("GetSessionEvents failed: %v", err)
		}

		if fetched != nil {
			t.Errorf("Expected nil slice for empty input, got %v", fetched)
		}
	})

	// Test 3: Fetch non-existent ID
	t.Run("Fetch non-existent ID", func(t *testing.T) {
		ctx := context.Background()
		fetched, err := store.GetSessionEvents(ctx, []string{"unknown"})
		if err != nil {
			t.Fatalf("GetSessionEvents failed: %v", err)
		}

		if len(fetched) != 0 {
			t.Errorf("Expected 0 events, got %d", len(fetched))
		}
	})
}
