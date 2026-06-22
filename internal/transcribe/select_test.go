package transcribe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSelectFallsBackToStub proves the honest-degradation contract: with no
// model on disk (and/or no `whisper` build tag), Select() returns the stub and
// the app still runs end-to-end. This runs on every build, tagged or not.
func TestSelectFallsBackToStub(t *testing.T) {
	t.Setenv("SCRIBE_ENGINE", "")
	// Point at a model path that definitely does not exist.
	t.Setenv("SCRIBE_MODEL_PATH", filepath.Join(t.TempDir(), "nope.bin"))

	eng := Select()
	if eng == nil {
		t.Fatal("Select() returned nil; must always return a usable engine")
	}
	if eng.Name() != "stub" {
		t.Fatalf("with no model present, expected stub engine, got %q", eng.Name())
	}

	segs, err := eng.Transcribe(context.Background(), "/tmp/example.wav")
	if err != nil {
		t.Fatalf("stub transcribe failed: %v", err)
	}
	if len(segs) == 0 {
		t.Fatal("stub returned no segments")
	}
}

// TestSelectEnvForcesStub proves SCRIBE_ENGINE=stub wins even if a model file
// exists — the CI / no-CGO escape hatch.
func TestSelectEnvForcesStub(t *testing.T) {
	// Create a real (empty) file at the model path so the stat() succeeds.
	mp := filepath.Join(t.TempDir(), "ggml-base.en.bin")
	if err := os.WriteFile(mp, []byte("not-a-real-model"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCRIBE_MODEL_PATH", mp)
	t.Setenv("SCRIBE_ENGINE", "stub")

	if got := Select().Name(); got != "stub" {
		t.Fatalf("SCRIBE_ENGINE=stub should force stub, got %q", got)
	}
}
