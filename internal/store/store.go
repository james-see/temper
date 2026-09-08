package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/james-see/temper/internal/event"
	_ "modernc.org/sqlite"
)

type Run struct {
	ID        string
	Goal      string
	State     string
	Agent     string
	Provider  string
	Model     string
	Workspace string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	goal TEXT NOT NULL,
	state TEXT NOT NULL,
	agent TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	workspace TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	sequence INTEGER NOT NULL,
	type TEXT NOT NULL,
	timestamp TEXT NOT NULL,
	actor TEXT NOT NULL,
	data TEXT,
	UNIQUE(run_id, sequence)
);
CREATE INDEX IF NOT EXISTS events_run_seq ON events(run_id, sequence);
`)
	return err
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.UpdatedAt = r.CreatedAt
	_, err := s.db.ExecContext(ctx, `
INSERT INTO runs (id, goal, state, agent, provider, model, workspace, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Goal, r.State, r.Agent, r.Provider, r.Model, r.Workspace,
		r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateRun(ctx context.Context, r Run) error {
	r.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE runs SET goal = ?, state = ?, agent = ?, provider = ?, model = ?, workspace = ?, updated_at = ?
WHERE id = ?`,
		r.Goal, r.State, r.Agent, r.Provider, r.Model, r.Workspace,
		r.UpdatedAt.Format(time.RFC3339Nano), r.ID)
	return err
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	var r Run
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT id, goal, state, agent, provider, model, workspace, created_at, updated_at
FROM runs WHERE id = ?`, id).Scan(
		&r.ID, &r.Goal, &r.State, &r.Agent, &r.Provider, &r.Model, &r.Workspace, &created, &updated)
	if err != nil {
		return Run{}, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

func (s *Store) AppendEvent(ctx context.Context, ev event.Event) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	data := []byte(ev.Data)
	if len(data) == 0 {
		data = []byte("null")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (id, run_id, sequence, type, timestamp, actor, data)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.RunID, ev.Sequence, ev.Type,
		ev.Timestamp.Format(time.RFC3339Nano), ev.Actor, string(data))
	return err
}

func (s *Store) NextSequence(ctx context.Context, runID string) (uint64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(sequence) FROM events WHERE run_id = ?`, runID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 1, nil
	}
	return uint64(seq.Int64) + 1, nil
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]event.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, sequence, type, timestamp, actor, data
FROM events WHERE run_id = ? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []event.Event
	for rows.Next() {
		var ev event.Event
		var ts, data string
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.Sequence, &ev.Type, &ts, &ev.Actor, &data); err != nil {
			return nil, err
		}
		ev.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		if data != "" && data != "null" {
			ev.Data = json.RawMessage(data)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) LastOfTypes(ctx context.Context, runID string, types []string) (event.Event, error) {
	if len(types) == 0 {
		return event.Event{}, sql.ErrNoRows
	}
	q := `SELECT id, run_id, sequence, type, timestamp, actor, data FROM events WHERE run_id = ? AND type IN (`
	args := []any{runID}
	for i, t := range types {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, t)
	}
	q += `) ORDER BY sequence DESC LIMIT 1`
	var ev event.Event
	var ts, data string
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&ev.ID, &ev.RunID, &ev.Sequence, &ev.Type, &ts, &ev.Actor, &data)
	if err != nil {
		return event.Event{}, err
	}
	ev.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
	if data != "" && data != "null" {
		ev.Data = json.RawMessage(data)
	}
	return ev, nil
}

func Encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func MustOpen(path string) *Store {
	s, err := Open(path)
	if err != nil {
		panic(fmt.Errorf("store: %w", err))
	}
	return s
}
