package transcribe

import (
	"os"
	"path/filepath"
)

// DefaultModelName is the model Scribe ships with by default: base.en is a good
// balance of accuracy and speed for English meeting/voice audio on Apple silicon.
// Download it with scripts/download-model.sh (it lands in models/ggml-base.en.bin,
// which is gitignored and never committed).
const DefaultModelName = "ggml-base.en.bin"

// ModelDir returns the directory Scribe looks in for whisper models. It is the
// repo-local models/ dir when running from source (resolved relative to the
// working dir); a packaged app overrides this via SCRIBE_MODEL_PATH.
func ModelDir() string {
	return "models"
}

// DefaultModelPath is the conventional on-disk location of the default model.
func DefaultModelPath() string {
	return filepath.Join(ModelDir(), DefaultModelName)
}

// Select returns the transcription engine to use, honoring three things in order:
//
//  1. SCRIBE_ENGINE=stub  -> always the deterministic stub (CI / no-model dev).
//  2. SCRIBE_MODEL_PATH    -> explicit model file to load (else DefaultModelPath).
//  3. build tag `whisper` + a readable model file -> the real whisper.cpp engine;
//     otherwise the stub.
//
// This is the single entrypoint main.go calls. It NEVER returns an error or nil:
// a missing model or a build without the `whisper` tag silently and honestly
// degrades to the stub, so the app always runs end-to-end. The active engine is
// reported via Transcriber.Name() ("stub" vs "whisper.cpp") in the UI.
func Select() Transcriber {
	if os.Getenv("SCRIBE_ENGINE") == "stub" {
		return NewStub()
	}

	modelPath := os.Getenv("SCRIBE_MODEL_PATH")
	if modelPath == "" {
		modelPath = DefaultModelPath()
	}

	if _, err := os.Stat(modelPath); err != nil {
		// No model on disk -> stub. Honest: the UI footer will say "stub".
		return NewStub()
	}

	// newWhisper is defined in whisper.go (tag `whisper`) or whisper_stub.go
	// (no tag). Without the tag it returns the stub even if a model exists,
	// because the engine was not compiled in.
	if eng, err := newWhisper(modelPath); err == nil && eng != nil {
		return eng
	}
	return NewStub()
}
