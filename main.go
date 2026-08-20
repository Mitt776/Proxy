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

	// Вторую копию не плодим: показываем окно уже работающей и уходим. Исключение —
	// перезапуск с повышением прав ради TUN: это наш же процесс, и предка мы уже
	// дождались выше. Если он всё-таки завис, стартуем всё равно — остаться без TUN
	// хуже, чем показать вторую копию на те секунды, что предок ещё жив.
	if !system.ClaimSingleInstance(app.onSecondInstance) && !hasFlag(tunAutostartFlag) {
		return
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "MitM",
		Width:  1180,
		Height: 760,
		// Своя шапка окна (перетаскивание — через --wails-draggable в TitleBar.svelte).
		Frameless: true,
		MinWidth:  1000,
		MinHeight: 660,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Цвет обязан совпадать с токеном --bg фронтенда: между стартом WebView2 и
		// первым кадром окно залито именно им, и расхождение видно вспышкой.
		BackgroundColour: &options.RGBA{R: 14, G: 10, B: 26, A: 1},
		StartHidden:      startHidden,
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
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
