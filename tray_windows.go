package main

import (
	_ "embed"
	"fmt"
	"sync"
	"sync/atomic"

	"Proxy/backend/config"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// Состояние меню трея. Пункты создаёт цикл сообщений systray (своя горутина), а
// обновляют его и наблюдатель за ядром, и вызовы из UI — поэтому весь доступ
// идёт под trayMu. Без него карта trayProfileIDs пишется и читается из разных
// потоков одновременно, а это не просто гонка, а фатальный «concurrent map read
// and map write», роняющий всё приложение.
var (
	trayMu           sync.Mutex
	trayConnect      *systray.MenuItem
	trayDisconnect   *systray.MenuItem
	trayModes        map[string]*systray.MenuItem // режим маршрутизации → пункт подменю
	trayProfiles     *systray.MenuItem            // подменю выбора активного профиля
	trayProfileIDs   map[*systray.MenuItem]string // пункт подменю → ID профиля
	trayProfileItems []*systray.MenuItem          // пункты подменю в порядке добавления
)

// trayReady — иконка в трее успешно поднялась. Читается из потока окна
// (beforeClose), пишется из потока systray, поэтому атомарный.
var trayReady atomic.Bool

// TrayReady сообщает, поднялась ли иконка в трее (используется в beforeClose,
// чтобы не спрятать окно, если трея нет — иначе его нечем будет вернуть).
func TrayReady() bool { return trayReady.Load() }

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
	connect := systray.AddMenuItem("Подключить", "Запустить прокси")
	disconnect := systray.AddMenuItem("Отключить", "Остановить прокси")

	// Подменю режима: переключается на лету через Clash API, ядро не трогаем.
	systray.AddSeparator()
	mMode := systray.AddMenuItem("Режим", "Как маршрутизировать трафик")
	modes := map[string]*systray.MenuItem{}
	for _, m := range []struct{ mode, title, tip string }{
		{config.ModeRule, "По правилам", "Работают правила маршрутизации"},
		{config.ModeGlobal, "Всё через прокси", "Игнорировать правила, весь трафик в туннель"},
		{config.ModeDirect, "Всё напрямую", "Игнорировать правила, прокси не используется"},
	} {
		item := mMode.AddSubMenuItemCheckbox(m.title, m.tip, false)
		mode := m.mode
		item.Click(func() {
			if err := a.SetMode(mode); err == nil {
				updateTrayMode(mode)
			}
		})
		modes[m.mode] = item
	}

	// Подменю профилей заполняется при каждом изменении списка.
	profiles := systray.AddMenuItem("Профиль", "Активный профиль")

	trayMu.Lock()
	trayConnect, trayDisconnect = connect, disconnect
	trayModes = modes
	trayProfiles = profiles
	trayProfileIDs = map[*systray.MenuItem]string{}
	trayMu.Unlock()

	a.rebuildTrayProfiles()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Выход", "Закрыть приложение")

	mShow.Click(func() { runtime.WindowShow(a.ctx) })
	connect.Click(func() {
		enableTUN := false
		if a.settings != nil {
			enableTUN = a.settings.Get().EnableTUN
		}
		_ = a.Connect(enableTUN)
	})
	disconnect.Click(func() { _ = a.Disconnect() })
	mQuit.Click(func() {
		a.trayQuit.Store(true)
		runtime.Quit(a.ctx)
	})

	trayReady.Store(true)
	updateTrayMode(a.GetMode())
	if a.manager != nil {
		updateTrayMenu(string(a.manager.State()))
	}
}

// rebuildTrayProfiles наполняет подменю профилей. energye/systray не умеет
// удалять пункты, поэтому существующие переиспользуем, а лишние прячем.
func (a *App) rebuildTrayProfiles() {
	if a.store == nil {
		return
	}
	// Список читаем до захвата trayMu: обращение к хранилищу берёт свои
	// блокировки, и держать здесь чужую нам ни к чему.
	profiles := a.store.List()
	activeID := a.store.ActiveID()

	trayMu.Lock()
	defer trayMu.Unlock()
	if trayProfiles == nil {
		return // трей ещё не поднялся
	}

	// Карта порядка не хранит — пункты держим отдельным списком.
	for i := len(trayProfileItems); i < len(profiles); i++ {
		item := trayProfiles.AddSubMenuItemCheckbox("", "", false)
		item.Click(func() {
			trayMu.Lock()
			id := trayProfileIDs[item]
			trayMu.Unlock()
			if id == "" {
				return
			}
			_ = a.SetActiveProfile(id) // он же перестроит подменю
		})
		trayProfileItems = append(trayProfileItems, item)
	}

	for i, item := range trayProfileItems {
		if i >= len(profiles) {
			trayProfileIDs[item] = ""
			item.Hide()
			continue
		}
		p := profiles[i]
		trayProfileIDs[item] = p.ID
		item.SetTitle(p.Name)
		item.Show()
		if p.ID == activeID {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	if len(profiles) == 0 {
		trayProfiles.Disable()
	} else {
		trayProfiles.Enable()
	}
}

// updateTrayMode отмечает галочкой текущий режим маршрутизации.
func updateTrayMode(mode string) {
	trayMu.Lock()
	defer trayMu.Unlock()
	for m, item := range trayModes {
		if m == mode {
			item.Check()
		} else {
			item.Uncheck()
		}
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

	trayMu.Lock()
	defer trayMu.Unlock()
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
	if !trayReady.Load() {
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
