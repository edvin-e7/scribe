// Package store is Scribe's on-disk persistence layer: recordings + transcript
// segments, with full-text search over segment text.
//
// It uses modernc.org/sqlite — the PURE-Go (CGO-free) SQLite, driver name
// "sqlite" (NOT "sqlite3"). This is what keeps Scribe cross-compilable and App
// Store-bundleable without a C toolchain. FTS5 is built in; loadable extensions
// are NOT available, so everything stays inside built-in modules. See README
// "Architecture & decisions".
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// Store wraps the SQLite connection. Construct with Open; always Close.
type Store struct {
	db *sql.DB
}

// Open opens (and migrates) the database at path. Use ":memory:" in tests.
//
// The DSN sets WAL + a busy_timeout so concurrent writers retry instead of
// failing with "database is locked" — modernc's locking is stricter in practice.
func Open(path string) (*Store, error) {
	// _pragma entries are applied per-connection by the modernc driver.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=synchronous(NORMAL)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single writer connection avoids cross-connection lock churn for this
	// desktop-scale workload; reads still go through the same pool fine.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the schema if absent. Safe to call repeatedly.
//
// The FTS5 virtual table indexes segment text. We keep it as a separate index
// table (not "content=") so the relational rows remain the source of truth and
// the FTS table is a pure search index we maintain on insert.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS recordings (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	title        TEXT NOT NULL,
	audio_path   TEXT NOT NULL,
	created_at   INTEGER NOT NULL, -- Unix nanoseconds
	duration_sec REAL NOT NULL DEFAULT 0,
	engine       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS segments (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	recording_id  INTEGER NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
	start_sec     REAL NOT NULL,
	end_sec       REAL NOT NULL,
	text          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_segments_recording ON segments(recording_id);

-- Full-text index over segment text. segment_id mirrors segments.id so a hit
-- maps back to its row. UNINDEXED keeps segment_id out of the matched columns.
CREATE VIRTUAL TABLE IF NOT EXISTS segments_fts USING fts5(
	text,
	segment_id UNINDEXED,
	tokenize = 'unicode61'
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// CreateRecording inserts a recording and returns it with its assigned ID.
// createdAt defaults to now (Unix nanoseconds) when zero.
func (s *Store) CreateRecording(title, audioPath, engine string, durationSec float64, createdAt int64) (Recording, error) {
	if createdAt == 0 {
		createdAt = time.Now().UnixNano()
	}
	res, err := s.db.Exec(
		`INSERT INTO recordings (title, audio_path, created_at, duration_sec, engine) VALUES (?, ?, ?, ?, ?)`,
		title, audioPath, createdAt, durationSec, engine,
	)
	if err != nil {
		return Recording{}, fmt.Errorf("insert recording: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Recording{}, fmt.Errorf("last insert id: %w", err)
	}
	return Recording{
		ID:          id,
		Title:       title,
		AudioPath:   audioPath,
		CreatedAt:   createdAt,
		DurationSec: durationSec,
		Engine:      engine,
	}, nil
}

// ReplaceSegments deletes any existing segments for a recording and inserts the
// given ones (keeping the FTS index in sync), inside one transaction. This is
// how a fresh transcription result is persisted.
func (s *Store) ReplaceSegments(recordingID int64, segs []Segment) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear old rows + their FTS entries.
	if _, err := tx.Exec(
		`DELETE FROM segments_fts WHERE segment_id IN (SELECT id FROM segments WHERE recording_id = ?)`,
		recordingID,
	); err != nil {
		return fmt.Errorf("clear fts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM segments WHERE recording_id = ?`, recordingID); err != nil {
		return fmt.Errorf("clear segments: %w", err)
	}

	for _, seg := range segs {
		res, err := tx.Exec(
			`INSERT INTO segments (recording_id, start_sec, end_sec, text) VALUES (?, ?, ?, ?)`,
			recordingID, seg.Start, seg.End, seg.Text,
		)
		if err != nil {
			return fmt.Errorf("insert segment: %w", err)
		}
		segID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("segment id: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO segments_fts (text, segment_id) VALUES (?, ?)`,
			seg.Text, segID,
		); err != nil {
			return fmt.Errorf("index segment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListRecordings returns all recordings, newest first. Ordering is explicit
// (created_at DESC, id DESC) so output is deterministic across machines —
// never trust SQLite's default row order.
func (s *Store) ListRecordings() ([]Recording, error) {
	rows, err := s.db.Query(
		`SELECT id, title, audio_path, created_at, duration_sec, engine
		 FROM recordings ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list recordings: %w", err)
	}
	defer rows.Close()

	out := make([]Recording, 0)
	for rows.Next() {
		var r Recording
		if err := rows.Scan(&r.ID, &r.Title, &r.AudioPath, &r.CreatedAt, &r.DurationSec, &r.Engine); err != nil {
			return nil, fmt.Errorf("scan recording: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SegmentsForRecording returns a recording's segments in transcript order.
// Ordering is explicit (start_sec, id) for determinism.
func (s *Store) SegmentsForRecording(recordingID int64) ([]Segment, error) {
	rows, err := s.db.Query(
		`SELECT id, recording_id, start_sec, end_sec, text
		 FROM segments WHERE recording_id = ? ORDER BY start_sec ASC, id ASC`,
		recordingID,
	)
	if err != nil {
		return nil, fmt.Errorf("segments: %w", err)
	}
	defer rows.Close()

	out := make([]Segment, 0)
	for rows.Next() {
		var seg Segment
		if err := rows.Scan(&seg.ID, &seg.RecordingID, &seg.Start, &seg.End, &seg.Text); err != nil {
			return nil, fmt.Errorf("scan segment: %w", err)
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

// Search runs an FTS5 query over segment text and returns matching segments with
// their parent recording info. Results are ordered by FTS relevance (rank) then
// by recording/start for a stable tie-break.
func (s *Store) Search(query string) ([]SearchHit, error) {
	rows, err := s.db.Query(
		`SELECT r.id, r.title, seg.id, seg.start_sec, seg.end_sec, seg.text
		 FROM segments_fts fts
		 JOIN segments seg ON seg.id = fts.segment_id
		 JOIN recordings r ON r.id = seg.recording_id
		 WHERE segments_fts MATCH ?
		 ORDER BY rank, r.created_at DESC, seg.start_sec ASC`,
		query,
	)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	out := make([]SearchHit, 0)
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.RecordingID, &h.RecordingTitle, &h.SegmentID, &h.Start, &h.End, &h.Text); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DeleteRecording removes a recording, its segments, and their FTS entries.
func (s *Store) DeleteRecording(recordingID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM segments_fts WHERE segment_id IN (SELECT id FROM segments WHERE recording_id = ?)`,
		recordingID,
	); err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM segments WHERE recording_id = ?`, recordingID); err != nil {
		return fmt.Errorf("delete segments: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM recordings WHERE id = ?`, recordingID); err != nil {
		return fmt.Errorf("delete recording: %w", err)
	}
	return tx.Commit()
}
