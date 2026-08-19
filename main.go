package main

import (
	"embed"
	"os"
	"strconv"
	"strings"
	"time"

	"Proxy/backend/system"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Перезапуск с повышением прав ради TUN: прежний процесс ещё жив (ShellExecuteW
	// вернул управление сразу после нашего старта) и держит общую WebView2 user data
	// folder. Ждём, пока он уйдёт, иначе движок не поднимется и окно будет пустым.
	if hasFlag(tunAutostartFlag) {
		waitForPredecessor()
	}

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

// waitForPredecessor даёт процессу, который нас перезапустил, спокойно завершиться
// и отпустить WebView2. Оба ожидания — с потолком по времени: если предок почему-то
// подвис, лучше стартовать с риском пустого окна, чем не стартовать вовсе.
func waitForPredecessor() {
	if pid, ok := flagInt(waitPidFlag); ok {
		system.WaitForProcessExit(pid, 10*time.Second)
	}
	system.WaitForWebviewRelease(5 * time.Second)
}

// flagInt читает числовое значение флага вида `--wait-pid=1234`.
func flagInt(name string) (int, bool) {
	prefix := name + "="
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, prefix) {
			continue
		}
		v, err := strconv.Atoi(strings.TrimPrefix(a, prefix))
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}
