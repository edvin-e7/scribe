//go:build whisper

package transcribe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-audio/wav"
)

// decodePCM16kMono returns audioPath as 16 kHz mono float32 samples in [-1, 1],
// which is exactly what whisper.cpp consumes.
//
// Strategy (cheapest first):
//
//  1. If the file is already a 16 kHz mono WAV, decode it directly in pure Go
//     (no external process) via go-audio/wav.
//  2. Otherwise — any other sample rate, channel count, or container (mp3/m4a/…)
//     — shell out to ffmpeg to transcode to a temp 16 kHz mono WAV, then decode
//     that. ffmpeg is OPTIONAL: if it isn't installed we return a clear,
//     actionable error rather than crashing, telling the user the format/SR
//     requirement.
func decodePCM16kMono(ctx context.Context, audioPath string) ([]float32, error) {
	if samples, ok, err := decodeWAVDirect(audioPath); err != nil {
		return nil, err
	} else if ok {
		return samples, nil
	}

	// Needs transcoding. ffmpeg is optional — degrade with a helpful message.
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf(
			"%q is not 16 kHz mono WAV and ffmpeg is not installed; "+
				"either install ffmpeg (brew install ffmpeg) or pre-convert with "+
				"`ffmpeg -i in -ar 16000 -ac 1 -c:a pcm_s16le out.wav`",
			filepath.Base(audioPath))
	}

	tmp, err := os.CreateTemp("", "scribe-*.wav")
	if err != nil {
		return nil, fmt.Errorf("create temp wav: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	// -y overwrite, mono, 16k, signed 16-bit PCM.
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", audioPath,
		"-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le",
		tmpPath,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg transcode failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	samples, ok, err := decodeWAVDirect(tmpPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("ffmpeg produced an unexpected WAV format for %q", filepath.Base(audioPath))
	}
	return samples, nil
}

// decodeWAVDirect decodes a WAV file to mono 16 kHz float32 if it already is one.
//
// ok=false means "not a 16 kHz mono WAV — caller should transcode"; err!=nil
// means the file claimed to be WAV but was malformed. Stereo or non-16k WAVs
// return ok=false so they take the ffmpeg path (we don't resample in Go).
func decodeWAVDirect(path string) (samples []float32, ok bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open audio: %w", err)
	}
	defer func() { _ = f.Close() }()

	dec := wav.NewDecoder(f)
	if !dec.IsValidFile() {
		return nil, false, nil // not a WAV; transcode path
	}

	buf, err := dec.FullPCMBuffer()
	if err != nil {
		return nil, false, fmt.Errorf("read pcm: %w", err)
	}
	if buf == nil || buf.Format == nil {
		return nil, false, nil
	}
	if buf.Format.SampleRate != whisperSampleRate || buf.Format.NumChannels != 1 {
		return nil, false, nil // wrong SR/channels -> transcode
	}

	// Convert integer PCM to float32 in [-1, 1]. go-audio gives us a float
	// buffer scaled to the source bit depth; normalize by it.
	fb := buf.AsFloatBuffer()
	bitDepth := buf.SourceBitDepth
	if bitDepth <= 0 {
		bitDepth = 16
	}
	scale := float64(int64(1) << (uint(bitDepth) - 1))
	out := make([]float32, len(fb.Data))
	for i, v := range fb.Data {
		s := v / scale
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = float32(s)
	}
	return out, true, nil
}
