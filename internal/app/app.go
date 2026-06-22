// Package app is Scribe's Wails binding layer: it wires the on-device transcriber
// to the store and exposes the methods the frontend calls. All int64 ids/timestamps
// cross the bridge as strings (see internal/store models) so JS can't round them.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/edvin-e7/scribe/internal/store"
	"github.com/edvin-e7/scribe/internal/transcribe"
)

// App holds the runtime dependencies. It is the struct bound into Wails.
type App struct {
	ctx    context.Context
	store  *store.Store
	engine transcribe.Transcriber
}

// New builds an App with a concrete store and transcriber. The transcriber is
// injected so swapping the stub for whisper.cpp is a one-line change at startup.
func New(s *store.Store, engine transcribe.Transcriber) *App {
	return &App{store: s, engine: engine}
}

// Startup is wired to Wails OnStartup; it captures the runtime context.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

// EngineName reports the active transcription engine (for the UI footer).
func (a *App) EngineName() string {
	return a.engine.Name()
}

// ImportAndTranscribe imports an audio file: it transcribes it on-device, stores
// the recording and its segments, and returns the created recording.
//
// title may be empty — the file's base name is used. This is the golden path the
// frontend's "Import" button hits today; "record" is a future block that ends in
// the same call with a freshly captured file.
func (a *App) ImportAndTranscribe(audioPath, title string) (store.Recording, error) {
	if audioPath == "" {
		return store.Recording{}, fmt.Errorf("audioPath is required")
	}
	if title == "" {
		title = filepath.Base(audioPath)
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	segs, err := a.engine.Transcribe(ctx, audioPath)
	if err != nil {
		return store.Recording{}, fmt.Errorf("transcribe: %w", err)
	}

	durationSec := 0.0
	if n := len(segs); n > 0 {
		durationSec = segs[n-1].End
	}

	rec, err := a.store.CreateRecording(title, audioPath, a.engine.Name(), durationSec, 0)
	if err != nil {
		return store.Recording{}, err
	}

	// Map engine segments to store segments (engine has no DB ids yet).
	storeSegs := make([]store.Segment, 0, len(segs))
	for _, s := range segs {
		storeSegs = append(storeSegs, store.Segment{
			RecordingID: rec.ID,
			Start:       s.Start,
			End:         s.End,
			Text:        s.Text,
		})
	}
	if err := a.store.ReplaceSegments(rec.ID, storeSegs); err != nil {
		return store.Recording{}, err
	}
	return rec, nil
}

// ListRecordings returns all recordings, newest first.
func (a *App) ListRecordings() ([]store.Recording, error) {
	return a.store.ListRecordings()
}

// GetSegments returns one recording's transcript in order. recordingID arrives as
// a string from JS (precision-safe) and is parsed back to int64.
func (a *App) GetSegments(recordingID string) ([]store.Segment, error) {
	id, err := parseID(recordingID)
	if err != nil {
		return nil, err
	}
	return a.store.SegmentsForRecording(id)
}

// Search runs full-text search over all transcript segments.
func (a *App) Search(query string) ([]store.SearchHit, error) {
	if query == "" {
		return []store.SearchHit{}, nil
	}
	return a.store.Search(query)
}

// DeleteRecording removes a recording and its transcript.
func (a *App) DeleteRecording(recordingID string) error {
	id, err := parseID(recordingID)
	if err != nil {
		return err
	}
	return a.store.DeleteRecording(id)
}

// parseID converts the string id from the bridge back to int64.
func parseID(s string) (int64, error) {
	var id int64
	if _, err := fmt.Sscan(s, &id); err != nil {
		return 0, fmt.Errorf("invalid id %q: %w", s, err)
	}
	return id, nil
}

// DefaultDBPath returns the per-user database path under the OS config dir,
// creating the directory. Audio and transcripts both stay local.
func DefaultDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "scribe")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "scribe.db"), nil
}
