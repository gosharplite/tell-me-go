package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSessionEvents_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnError(errors.New("connection failed"))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "querying session events")
	assert.Contains(t, err.Error(), "connection failed")
	assert.Nil(t, events)
}

func TestGetSessionEvents_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	r := newEventStore(db)
	// Passing a NULL (nil) to a non-nullable Scan target (string) will trigger a scan error
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", nil))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scanning event row")
	assert.Nil(t, events)
}

func TestGetSessionEvents_ParseTimeError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	r := newEventStore(db)
	eventID := "event-bad-time"
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs(eventID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow(eventID, "payload", "invalid-date"))

	events, err := r.getSessionEvents(context.Background(), []string{eventID})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("parsing created_at for event %s", eventID))
	assert.Nil(t, events)
}

func TestGetSessionEvents_IterationError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", "2023-10-27T10:00:00Z").
			RowError(0, errors.New("iteration failure")))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event rows iteration")
	assert.Contains(t, err.Error(), "iteration failure")
	assert.Nil(t, events)
}

func TestGetSessionEvents_CloseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", "2023-10-27T10:00:00Z").
			CloseError(errors.New("failed to close rows")))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	// We check for the specific error message. Depending on the mock/driver behavior,
	// it might be captured by rows.Err() or the explicit rows.Close() in defer.
	assert.Contains(t, err.Error(), "failed to close rows")
	assert.Nil(t, events)
}
