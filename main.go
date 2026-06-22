// Command scribe is the Wails desktop app: an on-device meeting/voice
// transcriber. Audio never leaves the machine.
package main

import (
	"embed"
	"log"

	"github.com/edvin-e7/scribe/internal/app"
	"github.com/edvin-e7/scribe/internal/store"
	"github.com/edvin-e7/scribe/internal/transcribe"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := app.DefaultDBPath()
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Engine selection. transcribe.Select() returns the real whisper.cpp engine
	// when the app was built with `-tags whisper` AND a model file is present
	// (models/ggml-base.en.bin, or $SCRIBE_MODEL_PATH); otherwise it degrades
	// honestly to the deterministic stub. Force the stub with SCRIBE_ENGINE=stub.
	engine := transcribe.Select()

	a := app.New(st, engine)

	err = wails.Run(&options.App{
		Title:  "Scribe",
		Width:  1100,
		Height: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 17, B: 19, A: 1},
		OnStartup:        a.Startup,
		Bind: []interface{}{
			a,
		},
	})
	if err != nil {
		log.Fatalf("wails run: %v", err)
	}
}
