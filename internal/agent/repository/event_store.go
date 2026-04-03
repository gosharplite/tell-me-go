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
func (r *EventStore) GetSessionEvents(ctx context.Context, eventIDs []string) ([]Event, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}

	query, args := buildSessionEventsQuery(eventIDs)
	
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events, err := parseSessionEvents(rows)
	if err != nil {
		return nil, err
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

func buildSessionEventsQuery(eventIDs []string) (string, []interface{}) {
	placeholders := strings.Repeat("?,", len(eventIDs)-1) + "?"
	query := "SELECT id, payload, created_at FROM events WHERE id IN (" + placeholders + ")"

	args := make([]interface{}, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	return query, args
}

func parseSessionEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var createdAtStr string
		if err := rows.Scan(&e.ID, &e.Payload, &createdAtStr); err != nil {
			return nil, err
		}

		// Parse the string time returned by SQLite
		if t, parseErr := time.Parse(time.RFC3339Nano, createdAtStr); parseErr == nil {
			e.CreatedAt = t
		}
		events = append(events, e)
	}
	return events, nil
}
