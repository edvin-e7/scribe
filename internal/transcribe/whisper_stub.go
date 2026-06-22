//go:build !whisper

package transcribe

// This file is compiled when the `whisper` build tag is ABSENT — i.e. the default
// `go build` / `go test` path, and any build without a C toolchain + whisper.cpp
// static libs. It keeps the package CGO-free and always green.
//
// newWhisper here never builds the real engine; it returns the stub so Select()
// degrades cleanly. The real implementation lives in whisper.go behind the
// `whisper` tag (cgo, links libwhisper). See README "whisper.cpp engine".

func newWhisper(modelPath string) (Transcriber, error) {
	_ = modelPath
	return NewStub(), nil
}
