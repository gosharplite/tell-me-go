package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestEventStore(t *testing.T) (*eventStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE events (
		id TEXT PRIMARY KEY,
		payload TEXT,
		created_at TEXT
	)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	now := time.Now().UTC()
	eventsData := []event{
		{ID: "event-1", Payload: `{"status": "started"}`, CreatedAt: now},
		{ID: "event-2", Payload: `{"status": "running"}`, CreatedAt: now.Add(time.Second)},
		{ID: "event-3", Payload: `{"status": "completed"}`, CreatedAt: now.Add(2 * time.Second)},
	}

	for _, e := range eventsData {
		_, err := db.Exec("INSERT INTO events (id, payload, created_at) VALUES (?, ?, ?)",
			e.ID, e.Payload, e.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("Failed to insert event %s: %v", e.ID, err)
		}
	}

	return newEventStore(db), db
}

func TestGetSessionEvents(t *testing.T) {
	store, db := setupTestEventStore(t)
	defer func() { _ = db.Close() }()

	tests := []struct {
		name        string
		ids         []string
		wantLen     int
		wantIDs     []string
		wantErr     bool
		wantNil     bool
	}{
		{
			name:    "Fetch multiple IDs",
			ids:     []string{"event-1", "event-3"},
			wantLen: 2,
			wantIDs: []string{"event-1", "event-3"},
			wantErr: false,
		},
		{
			name:    "Fetch empty slice",
			ids:     []string{},
			wantLen: 0,
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "Fetch non-existent ID",
			ids:     []string{"unknown"},
			wantLen: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fetched, err := store.getSessionEvents(ctx, tt.ids)

			assertEventResults(t, fetched, err, tt.wantErr, tt.wantNil, tt.wantLen, tt.wantIDs)
		})
	}
}

func assertEventResults(t *testing.T, fetched []event, err error, wantErr, wantNil bool, wantLen int, wantIDs []string) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Fatalf("GetSessionEvents error = %v, wantErr %v", err, wantErr)
	}

	if wantNil && fetched != nil {
		t.Errorf("Expected nil slice, got %v", fetched)
	}

	if len(fetched) != wantLen {
		t.Fatalf("Expected %d events, got %d", wantLen, len(fetched))
	}

	for _, wantID := range wantIDs {
		found := false
		for _, f := range fetched {
			if f.ID == wantID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find event ID %s", wantID)
		}
	}
}
