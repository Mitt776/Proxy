package main

import (
	_ "embed"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// Ссылки на пункты меню, чтобы обновлять их доступность из обработчика состояния.
var (
	trayConnect    *systray.MenuItem
	trayDisconnect *systray.MenuItem
	trayReady      bool // трей успешно инициализирован
)

// TrayReady сообщает, поднялась ли иконка в трее (используется в beforeClose,
// чтобы не спрятать окно, если трея нет — иначе его нечем будет вернуть).
func TrayReady() bool { return trayReady }

var trayTip = map[string]string{
	"stopped":  "отключено",
	"starting": "запуск…",
	"running":  "подключено",
	"error":    "ошибка",
}

// runTray запускает иконку в трее. energye/systray держит собственный цикл
// сообщений, поэтому вызывается из отдельной горутины.
func (a *App) runTray() {
	systray.Run(a.onTrayReady, nil)
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("Proxy")
	systray.SetTooltip("Proxy")

	mShow := systray.AddMenuItem("Показать окно", "Открыть окно приложения")
	systray.AddSeparator()
	trayConnect = systray.AddMenuItem("Подключить", "Запустить прокси")
	trayDisconnect = systray.AddMenuItem("Отключить", "Остановить прокси")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Выход", "Закрыть приложение")

	mShow.Click(func() { runtime.WindowShow(a.ctx) })
	trayConnect.Click(func() {
		enableTUN := false
		if a.settings != nil {
			enableTUN = a.settings.Get().EnableTUN
		}
		_ = a.Connect(enableTUN)
	})
	trayDisconnect.Click(func() { _ = a.Disconnect() })
	mQuit.Click(func() {
		a.trayQuit = true
		runtime.Quit(a.ctx)
	})

	trayReady = true
	if a.manager != nil {
		updateTrayMenu(string(a.manager.State()))
	}
}

// updateTrayMenu синхронизирует пункты меню и подсказку с состоянием ядра.
// Безопасно вызывать до готовности трея (пункты ещё nil).
func updateTrayMenu(state string) {
	tip := trayTip[state]
	if tip == "" {
		tip = state
	}
	systray.SetTooltip("Proxy — " + tip)

	if trayConnect == nil || trayDisconnect == nil {
		return
	}
	active := state == "running" || state == "starting"
	if active {
		trayConnect.Disable()
		trayDisconnect.Enable()
	} else {
		trayConnect.Enable()
		trayDisconnect.Disable()
	}
}

// stopTray завершает цикл трея при выходе приложения.
func stopTray() {
	systray.Quit()
}
