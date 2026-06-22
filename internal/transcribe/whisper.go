//go:build whisper

// Package transcribe — real on-device engine.
//
// This file is compiled ONLY with the `whisper` build tag:
//
//	go build  -tags "whisper fts5" ./...
//	go test   -tags "whisper fts5" ./...
//
// It is cgo and links whisper.cpp's static libs (libwhisper, libggml*). Building
// it therefore requires the C toolchain + whisper.cpp libraries on the cgo paths
// (C_INCLUDE_PATH / LIBRARY_PATH). The default build (no tag, see whisper_stub.go)
// stays pure-Go and green without any of that, which is what keeps CI honest.
//
// The engine is whisper.cpp via the OFFICIAL upstream Go binding
// (github.com/ggerganov/whisper.cpp/bindings/go) — chosen because it is the
// canonical, most-maintained, MIT-licensed binding, tracking whisper.cpp itself
// (no third-party staleness risk). See README "whisper.cpp engine".
package transcribe

import (
	"context"
	"fmt"
	"time"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

// Whisper is the real on-device Transcriber backed by whisper.cpp.
//
// It loads a ggml model once and reuses it across calls. Audio is decoded to
// 16 kHz mono float32 PCM (see audio.go) before being handed to whisper.cpp.
type Whisper struct {
	model     whisper.Model
	modelPath string
	language  string // "auto" or e.g. "en"; default "auto"
}

// newWhisper builds the real engine. It is the tagged counterpart of the shim in
// whisper_stub.go, so Select() resolves to whichever was compiled in.
func newWhisper(modelPath string) (Transcriber, error) {
	model, err := whisper.New(modelPath)
	if err != nil {
		return nil, fmt.Errorf("load whisper model %q: %w", modelPath, err)
	}
	return &Whisper{model: model, modelPath: modelPath, language: "auto"}, nil
}

// Name implements Transcriber.
func (w *Whisper) Name() string { return "whisper.cpp" }

// Close releases the model. Optional; the process owns one engine for its life.
func (w *Whisper) Close() error { return w.model.Close() }

// Transcribe implements Transcriber. It decodes audioPath to 16 kHz mono PCM,
// runs whisper.cpp, and returns ordered segments. ctx cancellation aborts the
// (slow) decode via the encoder-begin callback.
func (w *Whisper) Transcribe(ctx context.Context, audioPath string) ([]Segment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	samples, err := decodePCM16kMono(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("decode audio: %w", err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("decode audio: no samples in %q", audioPath)
	}

	wctx, err := w.model.NewContext()
	if err != nil {
		return nil, fmt.Errorf("new whisper context: %w", err)
	}
	if w.language != "" {
		// Best-effort: monolingual models (.en) reject SetLanguage; ignore.
		_ = wctx.SetLanguage(w.language)
	}

	// Abort the encode loop if ctx is cancelled.
	encoderBegin := func() bool { return ctx.Err() == nil }

	if err := wctx.Process(samples, encoderBegin, nil, nil); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("whisper process: %w", err)
	}

	var segs []Segment
	for {
		s, err := wctx.NextSegment()
		if err != nil {
			break // io.EOF at end of stream
		}
		segs = append(segs, Segment{
			Start: round2(s.Start.Seconds()),
			End:   round2(s.End.Seconds()),
			Text:  trimSpace(s.Text),
		})
	}
	return segs, nil
}

// trimSpace trims the leading space whisper.cpp emits on each segment without
// pulling in strings just for this.
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// Ensure Whisper satisfies the interface at compile time.
var _ Transcriber = (*Whisper)(nil)

// whisperSampleRate is what whisper.cpp expects: 16 kHz mono.
const whisperSampleRate = 16000

// (time is imported so the binding's time.Duration fields resolve cleanly even
// if the compiler prunes; keep an explicit reference.)
var _ = time.Second
