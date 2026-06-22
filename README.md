# Scribe

**On-device meeting & voice transcription.** Your owned, $0 alternative to
Otter.ai / Granola — audio is transcribed locally and never leaves your machine.
No cloud API, no upload, no per-request billing, no account.

> Working name. The app records or imports audio, transcribes it on-device,
> stores recordings + timestamped transcript segments, and gives you full-text
> search across everything you've ever captured.

Status: **real on-device engine landed + green build.** The transcription engine
is **whisper.cpp** (native, on-device); it is selected automatically once a model
is present and the app is built with `-tags whisper`, otherwise it degrades
honestly to a deterministic **stub** so the app — and CI — always runs end-to-end
without a C toolchain or a downloaded model. The UI is still a minimal functional
shell, not the final design (that's the design-pass block). See
[Roadmap](#roadmap).

> **Verified end-to-end** on macOS (Apple silicon): `base.en` transcribing the
> bundled `jfk.wav` sample through the `Transcriber` interface produced
> `And so my fellow Americans, ask not what your country can do for you, ask what
> you can do for your country.` — see [Engine status](#engine--verification-status).

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

  **Binding choice: the official upstream Go binding**
  (`github.com/ggerganov/whisper.cpp/bindings/go`). Evaluated against the
  alternatives — `go-skynet/go-whisper` (older, now effectively redirects to the
  same upstream binding) and the `mutablelogic` server-style wrappers (heavier, a
  whole HTTP service rather than a clean library). The upstream binding wins
  because it lives **inside the whisper.cpp repo itself**, so it never drifts from
  the C library it wraps, is MIT-licensed, and exposes exactly the high-level
  `Model` → `Context` → `Process(samples)` → `NextSegment()` surface this engine
  needs. It is cgo and links whisper.cpp's static libs — which is why it is gated
  behind a build tag (below), keeping the default build pure-Go.

  This lives behind a single Go interface, `transcribe.Transcriber`
  (`Transcribe(ctx, audioPath) ([]Segment, error)`). Two implementations satisfy
  it: the deterministic `Stub` and the real `whisper.cpp` engine. `transcribe.
  Select()` (called once in `main.go`) chooses between them — the rest of the app
  is engine-agnostic.

- **Why the engine is behind a build tag (`whisper`), with a stub fallback.** The
  real engine is cgo and needs whisper.cpp's compiled static libs + headers, which
  not every build environment (or CI runner) has. So:
    - **Default build** (`go build -tags fts5 ./...`) — no `whisper` tag, pure Go,
      no cgo, no model needed. `Select()` returns the **stub**. `go build` / `go vet`
      / `go test` are always green. This is the contract that keeps CI honest.
    - **Real build** (`go build -tags "whisper fts5" ./...`) — compiles the cgo
      engine. At runtime `Select()` returns whisper.cpp **iff** a model file is
      present (`models/ggml-base.en.bin` or `$SCRIBE_MODEL_PATH`); otherwise it
      *still* degrades to the stub. `SCRIBE_ENGINE=stub` forces the stub regardless.
      The active engine is reported in the UI footer via `EngineName()`.

- **Audio decoding (16 kHz mono PCM).** whisper.cpp wants 16 kHz mono float32 PCM.
  A file that is **already** a 16 kHz mono WAV is decoded in pure Go (no external
  process). Anything else (different sample rate, stereo, mp3/m4a/…) is transcoded
  via **ffmpeg** — which is **optional**: if ffmpeg isn't installed, Scribe returns
  a clear, actionable error (telling you to install ffmpeg or pre-convert) rather
  than crashing.

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
│   │   ├── stub.go               # deterministic offline fallback engine
│   │   ├── select.go             # Select(): picks whisper.cpp vs stub at startup
│   │   ├── whisper.go            # real whisper.cpp engine  (build tag: whisper, cgo)
│   │   ├── audio.go              # 16kHz mono PCM decode: native WAV / ffmpeg (tag: whisper)
│   │   ├── whisper_stub.go       # newWhisper shim when tag absent (keeps default pure-Go)
│   │   ├── select_test.go        # Select() falls back to stub (always runs)
│   │   └── whisper_test.go       # real engine; SKIPs cleanly w/o model (tag: whisper)
├── scripts/
│   └── download-model.sh         # $0 model fetch from HuggingFace (gitignored output)
├── models/                       # downloaded ggml models — gitignored, never committed
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

The `fts5` build tag is required (SQLite full-text search). Without a model and
the `whisper` tag, importing audio runs the **stub** engine (demo segments you can
browse and search).

### Real on-device engine (whisper.cpp)

```bash
# 1. Download a model ($0, no API key — from the public HuggingFace mirror).
#    Default is base.en (good accuracy/speed balance for English).
scripts/download-model.sh            # → models/ggml-base.en.bin  (gitignored)
scripts/download-model.sh small.en   # bigger/more accurate, optional

# 2. Build/run WITH the whisper tag. This is cgo and needs whisper.cpp's static
#    libs + headers on the cgo paths. From source you build them once with cmake:
#      cmake -S <whisper.cpp> -B build -DBUILD_SHARED_LIBS=OFF && cmake --build build
#    then point cgo at them:
export C_INCLUDE_PATH="<whisper.cpp>/include:<whisper.cpp>/ggml/include"
export LIBRARY_PATH="<build>/src:<build>/ggml/src:<build>/ggml/src/ggml-metal:<build>/ggml/src/ggml-blas"
go build -tags "whisper fts5" ./...
# or:  wails build -tags "whisper fts5"
```

At runtime the engine self-selects: whisper.cpp if a model is present, else the
stub. Force the stub anytime with `SCRIBE_ENGINE=stub`. Audio that isn't already
16 kHz mono WAV is transcoded via ffmpeg (optional; install `brew install ffmpeg`).

### Verify

```bash
# Default path (no cgo, no model) — always green, this is what CI runs:
go build -tags fts5 ./...     # backend compiles
go vet   -tags fts5 ./...     # static checks
go test  -tags fts5 ./...     # FTS5 round-trip + Select()-falls-back-to-stub
cd frontend && npm run build  # frontend compiles (tsc + vite)

# Real engine (after download-model.sh + cgo paths exported as above):
SCRIBE_MODEL_PATH=$PWD/models/ggml-base.en.bin \
  go test -tags "whisper fts5" -run TestWhisper ./internal/transcribe/
# SKIPs cleanly if no model is on disk — never falsely green.
```

### Engine — verification status

| What | Status |
|------|--------|
| Default (no-tag) build / vet / test | ✅ verified green (pure Go, no model needed) |
| `Select()` degrades to stub w/o model | ✅ verified (`select_test.go`) |
| whisper.cpp cgo engine compiles (`-tags whisper`) | ✅ verified (libs built via cmake, links clean) |
| **Real transcription end-to-end** (base.en, jfk.wav) | ✅ verified — output below |
| Recording / mic capture | ⬜ not built (Roadmap #3) |
| Model bundled into the `.app` | ⬜ not done (downloaded at runtime today; see App Store note) |

Verified transcription of the bundled `jfk.wav` (16 kHz mono, native WAV decode
path, `base.en`, Apple-silicon Metal):

```
[0.00-11.00] And so my fellow Americans, ask not what your country can do for you,
             ask what you can do for your country.
```

That is the correct reference transcription — the engine works end-to-end through
the `Transcriber` interface, not just in isolation.

### App Store distribution note

Today the model is **downloaded at runtime** into `models/` (gitignored). For a
shipped `.app` the model should be **bundled into the bundle's Resources** (or
downloaded on first launch into Application Support) and resolved via
`$SCRIBE_MODEL_PATH` / a packaged-path lookup, so the signed/notarized app is
self-contained with no first-run network requirement. whisper.cpp links
statically, so there is no runtime/dylib to sign separately — that's a key reason
it was chosen over a Python sidecar. Bundling is a packaging block, not an engine
change.

---

## Roadmap

Honest, in order:

1. **✅ Scaffold + green build (this commit).** Wails app, pure-Go SQLite + FTS5,
   `Transcriber` interface with a deterministic stub, minimal functional UI
   (import → transcript view → search), one falsifying FTS5 round-trip test.
2. **✅ whisper.cpp engine (this commit).** Real on-device engine via the official
   upstream Go binding, behind the `whisper` build tag (cgo). `Select()` chooses
   it when a model is present, else the stub. Model download script (`$0`, no key),
   16 kHz mono PCM decode (native WAV + optional ffmpeg), runs on Metal. Stub kept
   for tests/CI; verified end-to-end on base.en/jfk.wav. **Next:** bundle the model
   into the signed `.app` (App Store note above).
3. **Recording.** Live microphone + system-audio capture (ScreenCaptureKit for
   meeting audio), writing a local file the same import path already consumes.
4. **Design pass.** Studio-grade UI via the design loop + review — the current
   shell is intentionally plain and will be rebuilt, not recolored.

---

## License & privacy

Personal project. The database and any audio live only on your machine and are
gitignored. No telemetry, no network calls in the transcription path.
