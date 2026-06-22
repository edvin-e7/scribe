// Package transcribe defines the on-device transcription contract for Scribe.
//
// The whole point of Scribe is that audio never leaves the machine: no cloud
// API, no upload, no per-request billing. Every concrete Transcriber here is an
// on-device engine. Today only a deterministic Stub exists so the app builds and
// runs end-to-end; the real engine (whisper.cpp, native, no Python runtime — see
// README "Architecture & decisions") slots in behind this same interface.
package transcribe

import "context"

// Segment is one contiguous span of transcribed speech.
//
// Start and End are offsets from the beginning of the recording, in seconds
// (float64 so sub-second boundaries survive). They are deliberately NOT exposed
// as raw nanosecond int64 — see internal/store for the int64-over-JSON gotcha.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Transcriber turns an on-disk audio file into ordered transcript segments.
//
// Implementations MUST:
//   - run fully on-device (no network);
//   - honor ctx cancellation (real engines are long-running);
//   - return segments sorted by Start ascending.
type Transcriber interface {
	// Transcribe reads the audio at audioPath and returns its segments.
	Transcribe(ctx context.Context, audioPath string) ([]Segment, error)

	// Name identifies the engine for UI / diagnostics (e.g. "stub", "whisper.cpp").
	Name() string
}
