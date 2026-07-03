package main

import (
	_ "embed"
	"fmt"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
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

// updateTraySpeed показывает текущую скорость в подсказке иконки трея.
func updateTraySpeed(down, up int64) {
	if !trayReady {
		return
	}
	systray.SetTooltip(fmt.Sprintf("Proxy ↓ %s ↑ %s", fmtRate(down), fmtRate(up)))
}

// trayNotify показывает всплывающее уведомление Windows (best-effort).
func trayNotify(title, body string) {
	go func() {
		n := toast.Notification{
			AppID:    "Proxy",
			Title:    title,
			Body:     body,
			Duration: toast.Short,
		}
		_ = n.Push() // не критично, если система не показала
	}()
}

// fmtRate форматирует скорость (байт/с) в человекочитаемый вид.
func fmtRate(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B/s", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	i := -1
	for v >= u && i < len(units)-1 {
		v /= u
		i++
	}
	return fmt.Sprintf("%.1f %s/s", v, units[i])
}

// stopTray завершает цикл трея при выходе приложения.
func stopTray() {
	systray.Quit()
}
