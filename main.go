package main

import (
	"embed"

	"Proxy/backend/system"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// При автозапуске (--minimized) стартуем скрытыми в трее.
	startHidden := hasFlag(system.MinimizedFlag)

	// Single instance: вторая копия не запускается, а показывает уже открытое окно.
	// Исключение — перезапуск с повышением прав ради TUN (это наш же процесс, ему
	// нужно стартовать, пока прежний ещё закрывается).
	var singleInstance *options.SingleInstanceLock
	if !hasFlag(tunAutostartFlag) {
		singleInstance = &options.SingleInstanceLock{
			UniqueId:               "proxy-singbox-client-1f6a2b",
			OnSecondInstanceLaunch: app.onSecondInstance,
		}
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Proxy",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:   &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		StartHidden:        startHidden,
		SingleInstanceLock: singleInstance,
		OnStartup:          app.startup,
		OnShutdown:         app.shutdown,
		OnBeforeClose:      app.beforeClose,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
