package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Event represents an orchestration event.
type Event struct {
	ID        string
	Payload   string
	CreatedAt time.Time
}

// EventStore handles database access for events.
type EventStore struct {
	db *sql.DB
}

// NewEventStore creates a new EventStore.
func NewEventStore(db *sql.DB) *EventStore {
	return &EventStore{db: db}
}

// GetSessionEvents fetches multiple events in a single database round-trip
// to prevent N+1 query bottlenecks.
func (r *EventStore) GetSessionEvents(ctx context.Context, eventIDs []string) (events []Event, err error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}

	// Dynamically build the IN clause with placeholders.
	placeholders := strings.Repeat("?,", len(eventIDs)-1) + "?"
	query := "SELECT id, payload, created_at FROM events WHERE id IN (" + placeholders + ")"

	args := make([]interface{}, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}()

	for rows.Next() {
		var e Event
		var createdAtStr string
		if err := rows.Scan(&e.ID, &e.Payload, &createdAtStr); err != nil {
			return nil, err
		}

		// Parse the string time returned by SQLite
		t, parseErr := time.Parse(time.RFC3339Nano, createdAtStr)
		if parseErr == nil {
			e.CreatedAt = t
		}
		events = append(events, e)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}
