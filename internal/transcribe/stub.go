package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"path/filepath"
)

// Stub is a deterministic, offline Transcriber used until whisper.cpp lands.
//
// It does NOT decode audio. It derives a stable, file-dependent set of demo
// segments from the audio path so the app has real-looking transcript data to
// render, save, and search — and so the same file always yields the same output
// (deterministic = testable, no flakiness). This is intentionally honest: it is
// a placeholder behind the real Transcriber interface, never presented as a real
// transcription. See README "Roadmap".
type Stub struct{}

// NewStub returns the deterministic demo engine.
func NewStub() *Stub { return &Stub{} }

// Name implements Transcriber.
func (s *Stub) Name() string { return "stub" }

// stubLines are the demo transcript sentences. The count of emitted segments and
// which lines are used are derived deterministically from the audio path.
var stubLines = []string{
	"Welcome to Scribe — this is a placeholder transcript.",
	"The real engine is whisper.cpp, running fully on-device.",
	"No audio ever leaves your machine: privacy is the architecture.",
	"Each segment carries a start and end timestamp in seconds.",
	"Full-text search runs over these segments via SQLite FTS5.",
	"Swap this stub for the real transcriber behind one interface.",
}

// Transcribe implements Transcriber. It is offline and deterministic: the same
// audioPath always produces the same segments. ctx is honored for cancellation
// so callers can treat it like the real (slow) engine.
func (s *Stub) Transcribe(ctx context.Context, audioPath string) ([]Segment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Derive a stable seed from the file path so output is file-dependent but
	// reproducible. base name only, so a moved file still transcribes the same.
	sum := sha256.Sum256([]byte(filepath.Base(audioPath)))
	seed := binary.BigEndian.Uint64(sum[:8])

	// 3..len(stubLines) segments, deterministically chosen from the seed.
	n := 3 + int(seed%uint64(len(stubLines)-2))
	if n > len(stubLines) {
		n = len(stubLines)
	}

	segs := make([]Segment, 0, n)
	cursor := 0.0
	for i := 0; i < n; i++ {
		// Each segment is 2.5..6.0s, derived per-index from the seed.
		dur := 2.5 + float64((seed>>(uint(i)*4))%36)/10.0
		segs = append(segs, Segment{
			Start: round2(cursor),
			End:   round2(cursor + dur),
			Text:  stubLines[i%len(stubLines)],
		})
		cursor += dur
	}
	return segs, nil
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// Ensure Stub satisfies the interface at compile time.
var _ Transcriber = (*Stub)(nil)
