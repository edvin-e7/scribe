# Scribe

**On-device meeting & voice transcription.** Your owned, $0 alternative to
Otter.ai / Granola — audio is transcribed locally and never leaves your machine.
No cloud API, no upload, no per-request billing, no account.

> Working name. The app records or imports audio, transcribes it on-device,
> stores recordings + timestamped transcript segments, and gives you full-text
> search across everything you've ever captured.

Status: **scaffold + green build.** The transcription engine is a deterministic
**stub** today; the real on-device engine (whisper.cpp) is the next block. See
[Roadmap](#roadmap). This is honest placeholder work — the stub is never
presented as a real transcription, and the UI is a minimal functional shell, not
the final design.

---

## Architecture & decisions

**Stack:** Wails v2 (Go backend + React/TypeScript frontend), SQLite via
`modernc.org/sqlite` (pure Go), FTS5 for search.

- **Why Wails (Go + React) for a native macOS app.** It produces a real native
  macOS bundle with a Go core and a web UI, with no Electron/Chromium runtime to
  ship and no Python. It is the established native-macOS pattern in this codebase,
  so the build, packaging, and App Store path are already proven. The Go core is
  where the on-device ML lives; React is only the surface.

- **Why whisper.cpp over a faster-whisper sidecar (the engine decision).** Both
  run Whisper locally. faster-whisper is a Python package — shipping it means
  bundling a Python runtime + native wheels (CTranslate2) inside the macOS app,
  which is fragile to sign/notarize and bloats the bundle. **whisper.cpp is a
  native C/C++ library** with no runtime: it compiles into the app, runs on the
  Apple Neural Engine / Metal, and produces a clean, signable App Store bundle.
  For a privacy-first product whose whole pitch is "nothing leaves your machine
  and there's no runtime to trust," a native engine is the right call. The
  Whisper model weights are downloaded once and cached locally.

  Today this lives behind a single Go interface, `transcribe.Transcriber`
  (`Transcribe(ctx, audioPath) ([]Segment, error)`), with a deterministic `Stub`
  implementation so the app builds and runs end-to-end now. Swapping in the real
  engine is one line in `main.go` — the rest of the app is engine-agnostic.

- **Why pure-Go SQLite (`modernc.org/sqlite`, driver `"sqlite"`).** It is a
  CGO-free transpilation of SQLite, so the app cross-compiles and distributes
  **without a C toolchain** — which is exactly what keeps the macOS/App Store
  build clean. FTS5 is built in (full-text search over transcript text). The
  trade-off: no loadable extensions, so everything stays inside built-in modules
  (FTS5, JSON1) — which is all Scribe needs. The DSN sets `journal_mode(WAL)` +
  `busy_timeout(5000)` because modernc's locking is stricter in practice.

- **Privacy / on-device POV.** This is the architectural thesis, not a feature
  bullet. Audio is read from local disk, transcribed by a local engine, and
  stored in a local SQLite database under the user's config dir. There is no
  network path in the transcription pipeline by construction. The DB and audio
  are gitignored and never leave the device.

- **int64 over the JS boundary (a deliberate correctness decision).** Recording
  IDs and `createdAt` (Unix **nanoseconds**, ~1.7e18) exceed JavaScript's safe
  integer ceiling (2^53). If sent as JSON numbers they'd be silently rounded,
  letting the frontend update or delete the wrong row. So every int64 id/timestamp
  is marshalled as a **string** across the Wails bridge (`json:"id,string"`), and
  the frontend treats them as opaque strings. Segment start/end stay `float64`
  seconds, which are safely in range.

- **Deterministic, explicit ordering.** All list queries sort explicitly
  (recordings by `created_at DESC, id DESC`; segments by `start_sec, id`) — never
  trusting SQLite's default row order, so output is stable across machines.

### Layout

```
scribe/
├── main.go                       # Wails entrypoint; injects store + engine
├── wails.json                    # Wails config (build:tags fts5)
├── internal/
│   ├── transcribe/
│   │   ├── transcribe.go         # Transcriber interface + Segment
│   │   └── stub.go               # deterministic offline placeholder engine
│   ├── store/
│   │   ├── models.go             # Recording, Segment, SearchHit (int64 as string)
│   │   ├── store.go              # SQLite + FTS5: create/list/search/delete
│   │   └── store_test.go         # FTS5 round-trip (present hits / absent misses)
│   └── app/
│       └── app.go                # Wails bindings: import→transcribe→store→search
└── frontend/                     # Vite + React + TS (base: './')
    ├── src/{App.tsx, api.ts, main.tsx, styles.css}
    └── …
```

---

## Run locally

Requires Go ≥ 1.24, Node ≥ 20, and the [Wails v2 CLI](https://wails.io)
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```bash
# Dev (hot reload):
wails dev

# Production build → build/bin/Scribe.app:
wails build -tags fts5
```

The `fts5` build tag is required (SQLite full-text search). Import an audio file
by pasting its path into the import field — today this runs the stub engine and
produces demo transcript segments you can browse and search.

### Verify

```bash
go build -tags fts5 ./...     # backend compiles
go vet   -tags fts5 ./...     # static checks
go test  -tags fts5 ./...     # FTS5 round-trip test
cd frontend && npm run build  # frontend compiles (tsc + vite)
```

---

## Roadmap

Honest, in order:

1. **✅ Scaffold + green build (this commit).** Wails app, pure-Go SQLite + FTS5,
   `Transcriber` interface with a deterministic stub, minimal functional UI
   (import → transcript view → search), one falsifying FTS5 round-trip test.
2. **whisper.cpp engine.** Vendor whisper.cpp, add a `WhisperTranscriber`
   implementing `transcribe.Transcriber`, wire model download/caching, decode
   audio (ffmpeg or a Go decoder) to 16 kHz PCM, run on Metal/ANE. Swap the one
   line in `main.go`. Keep the stub for tests/CI.
3. **Recording.** Live microphone + system-audio capture (ScreenCaptureKit for
   meeting audio), writing a local file the same import path already consumes.
4. **Design pass.** Studio-grade UI via the design loop + review — the current
   shell is intentionally plain and will be rebuilt, not recolored.

---

## License & privacy

Personal project. The database and any audio live only on your machine and are
gitignored. No telemetry, no network calls in the transcription path.
