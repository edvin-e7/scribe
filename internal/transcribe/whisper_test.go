//go:build whisper

package transcribe

import (
	"context"
	"os"
	"testing"
)

// modelPathForTest resolves the model the real-engine tests use, or "" if none.
func modelPathForTest() string {
	if p := os.Getenv("SCRIBE_MODEL_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p := DefaultModelPath(); fileExists(p) {
		return p
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestWhisperConstructor exercises the REAL engine constructor. It SKIPS cleanly
// (never falsely green) when no model is present — downloading a multi-hundred-MB
// model is not a unit-test side effect. To run it for real:
//
//	scripts/download-model.sh base.en
//	go test -tags "whisper fts5" -run TestWhisper ./internal/transcribe/
func TestWhisperConstructor(t *testing.T) {
	mp := modelPathForTest()
	if mp == "" {
		t.Skip("no whisper model on disk (run scripts/download-model.sh); skipping real-engine test")
	}

	eng, err := newWhisper(mp)
	if err != nil {
		t.Fatalf("newWhisper(%q) failed: %v", mp, err)
	}
	if eng == nil {
		t.Fatal("newWhisper returned nil engine without error")
	}
	if eng.Name() != "whisper.cpp" {
		t.Fatalf("expected engine name whisper.cpp, got %q", eng.Name())
	}
	if c, ok := eng.(*Whisper); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
}

// TestWhisperTranscribeSample runs an actual transcription IF both a model and a
// sample WAV are present. It Skips otherwise — falsifiable, never fake-green.
// Provide a 16 kHz mono WAV via SCRIBE_TEST_WAV.
func TestWhisperTranscribeSample(t *testing.T) {
	mp := modelPathForTest()
	if mp == "" {
		t.Skip("no whisper model on disk; skipping")
	}
	wavPath := os.Getenv("SCRIBE_TEST_WAV")
	if wavPath == "" || !fileExists(wavPath) {
		t.Skip("no sample WAV (set SCRIBE_TEST_WAV to a 16kHz mono wav); skipping")
	}

	eng, err := newWhisper(mp)
	if err != nil {
		t.Fatalf("newWhisper failed: %v", err)
	}
	if c, ok := eng.(*Whisper); ok {
		t.Cleanup(func() { _ = c.Close() })
	}

	segs, err := eng.Transcribe(context.Background(), wavPath)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	if len(segs) == 0 {
		t.Fatal("real engine returned no segments for a non-empty sample")
	}
	for _, s := range segs {
		t.Logf("[%.2f-%.2f] %s", s.Start, s.End, s.Text)
	}
}
