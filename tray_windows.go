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
	trayShow         *systray.MenuItem
	trayConnect      *systray.MenuItem
	trayDisconnect   *systray.MenuItem
	trayModeRoot     *systray.MenuItem            // подменю «Режим»
	trayModes        map[string]*systray.MenuItem // режим маршрутизации → пункт подменю
	trayProfiles     *systray.MenuItem            // подменю выбора активного профиля
	trayProfileIDs   map[*systray.MenuItem]string // пункт подменю → ID профиля
	trayProfileItems []*systray.MenuItem          // пункты подменю в порядке добавления
	trayQuitItem     *systray.MenuItem
)

// trayReady — иконка в трее успешно поднялась. Читается из потока окна
// (beforeClose), пишется из потока systray, поэтому атомарный.
var trayReady atomic.Bool

// TrayReady сообщает, поднялась ли иконка в трее (используется в beforeClose,
// чтобы не спрятать окно, если трея нет — иначе его нечем будет вернуть).
func TrayReady() bool { return trayReady.Load() }

// trayStrings — все подписи меню трея на одном языке. Меню живёт вне фронтенда,
// поэтому его строки не могут прийти из i18n-стора и дублируются здесь.
type trayStrings struct {
	Show, ShowTip             string
	Connect, ConnectTip       string
	Disconnect, DisconnectTip string
	Mode, ModeTip             string
	ModeRule, ModeRuleTip     string
	ModeGlobal, ModeGlobalTip string
	ModeDirect, ModeDirectTip string
	Profile, ProfileTip       string
	Quit, QuitTip             string
	StateStopped              string
	StateStarting             string
	StateRunning              string
	StateError                string
	NotifyUpTitle             string
	NotifyUpBody              string
	NotifyLostTitle           string
	NotifyLostBody            string
}

var trayText = map[string]trayStrings{
	"ru": {
		Show: "Показать окно", ShowTip: "Открыть окно приложения",
		Connect: "Подключить", ConnectTip: "Запустить прокси",
		Disconnect: "Отключить", DisconnectTip: "Остановить прокси",
		Mode: "Режим", ModeTip: "Как маршрутизировать трафик",
		ModeRule: "По правилам", ModeRuleTip: "Работают правила маршрутизации",
		ModeGlobal: "Всё через прокси", ModeGlobalTip: "Игнорировать правила, весь трафик в туннель",
		ModeDirect: "Всё напрямую", ModeDirectTip: "Игнорировать правила, прокси не используется",
		Profile: "Профиль", ProfileTip: "Активный профиль",
		Quit: "Выход", QuitTip: "Закрыть приложение",
		StateStopped: "отключено", StateStarting: "запуск…",
		StateRunning: "подключено", StateError: "ошибка",
		NotifyUpTitle: "Подключено", NotifyUpBody: "Прокси активен",
		NotifyLostTitle: "Соединение разорвано",
		NotifyLostBody:  "Прокси отключился — трафик идёт напрямую",
	},
	"en": {
		Show: "Show window", ShowTip: "Open the application window",
		Connect: "Connect", ConnectTip: "Start the proxy",
		Disconnect: "Disconnect", DisconnectTip: "Stop the proxy",
		Mode: "Mode", ModeTip: "How to route traffic",
		ModeRule: "By rules", ModeRuleTip: "Routing rules are in effect",
		ModeGlobal: "Everything via proxy", ModeGlobalTip: "Ignore rules, send all traffic to the tunnel",
		ModeDirect: "Everything direct", ModeDirectTip: "Ignore rules, do not use the proxy",
		Profile: "Profile", ProfileTip: "Active profile",
		Quit: "Quit", QuitTip: "Close the application",
		StateStopped: "disconnected", StateStarting: "starting…",
		StateRunning: "connected", StateError: "error",
		NotifyUpTitle: "Connected", NotifyUpBody: "Proxy is active",
		NotifyLostTitle: "Connection lost",
		NotifyLostBody:  "Proxy stopped — traffic goes direct",
	},
}

// trayLang — язык меню трея. Отдельно от trayMu и атомарно: подписи читаются в том
// числе из updateTrayMenu, которая уже держит trayMu, — общая блокировка на оба
// состояния означала бы гарантированный дедлок.
var trayLang atomic.Value // string

// trayT — подписи на текущем языке (по умолчанию русские).
func trayT() trayStrings {
	l, _ := trayLang.Load().(string)
	if s, ok := trayText[l]; ok {
		return s
	}
	return trayText["ru"]
}

// trayStateTip — короткое описание состояния ядра для тултипа иконки.
func trayStateTip(state string) string {
	t := trayT()
	switch state {
	case "stopped":
		return t.StateStopped
	case "starting":
		return t.StateStarting
	case "running":
		return t.StateRunning
	case "error":
		return t.StateError
	}
	return state
}

// applyTrayLang переводит уже созданное меню. energye/systray не умеет удалять и
// пересоздавать пункты, зато умеет их переименовывать — тем же приёмом, что и
// rebuildTrayProfiles. Поэтому смена языка обходится без перезапуска трея.
func applyTrayLang(lang string) {
	trayLang.Store(lang)
	t := trayT()

	trayMu.Lock()
	defer trayMu.Unlock()
	setTrayTitle(trayShow, t.Show, t.ShowTip)
	setTrayTitle(trayConnect, t.Connect, t.ConnectTip)
	setTrayTitle(trayDisconnect, t.Disconnect, t.DisconnectTip)
	setTrayTitle(trayModeRoot, t.Mode, t.ModeTip)
	setTrayTitle(trayModes[config.ModeRule], t.ModeRule, t.ModeRuleTip)
	setTrayTitle(trayModes[config.ModeGlobal], t.ModeGlobal, t.ModeGlobalTip)
	setTrayTitle(trayModes[config.ModeDirect], t.ModeDirect, t.ModeDirectTip)
	setTrayTitle(trayProfiles, t.Profile, t.ProfileTip)
	setTrayTitle(trayQuitItem, t.Quit, t.QuitTip)
}

// setTrayTitle безопасно переименовывает пункт (до готовности трея он nil).
func setTrayTitle(item *systray.MenuItem, title, tip string) {
	if item == nil {
		return
	}
	item.SetTitle(title)
	item.SetTooltip(tip)
}

// runTray запускает иконку в трее. energye/systray держит собственный цикл
// сообщений, поэтому вызывается из отдельной горутины.
func (a *App) runTray() {
	systray.Run(a.onTrayReady, nil)
}

func (a *App) onTrayReady() {
	t := trayT()

	systray.SetIcon(trayIcon)
	systray.SetTitle(appName)
	systray.SetTooltip(appName)

	mShow := systray.AddMenuItem(t.Show, t.ShowTip)
	systray.AddSeparator()
	connect := systray.AddMenuItem(t.Connect, t.ConnectTip)
	disconnect := systray.AddMenuItem(t.Disconnect, t.DisconnectTip)

	// Подменю режима: переключается на лету через Clash API, ядро не трогаем.
	systray.AddSeparator()
	mMode := systray.AddMenuItem(t.Mode, t.ModeTip)
	modes := map[string]*systray.MenuItem{}
	for _, m := range []struct{ mode, title, tip string }{
		{config.ModeRule, t.ModeRule, t.ModeRuleTip},
		{config.ModeGlobal, t.ModeGlobal, t.ModeGlobalTip},
		{config.ModeDirect, t.ModeDirect, t.ModeDirectTip},
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
	profiles := systray.AddMenuItem(t.Profile, t.ProfileTip)

	trayMu.Lock()
	trayShow = mShow
	trayConnect, trayDisconnect = connect, disconnect
	trayModeRoot = mMode
	trayModes = modes
	trayProfiles = profiles
	trayProfileIDs = map[*systray.MenuItem]string{}
	trayMu.Unlock()

	a.rebuildTrayProfiles()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem(t.Quit, t.QuitTip)
	trayMu.Lock()
	trayQuitItem = mQuit
	trayMu.Unlock()

	mShow.Click(func() { runtime.WindowShow(a.ctx) })
	connect.Click(func() {
		enableTUN := false
		if a.core != nil {
			enableTUN = a.core.GetSettings().EnableTUN
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
	if a.core == nil {
		return
	}
	// Список читаем до захвата trayMu: обращение к хранилищу берёт свои
	// блокировки, и держать здесь чужую нам ни к чему.
	profiles := a.core.ListProfiles()
	activeID := a.core.GetActiveProfileID()

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
	systray.SetTooltip(appName + " — " + trayStateTip(state))

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
	systray.SetTooltip(fmt.Sprintf("%s ↓ %s ↑ %s", appName, fmtRate(down), fmtRate(up)))
}

// trayNotify показывает всплывающее уведомление Windows (best-effort).
func trayNotify(title, body string) {
	go func() {
		n := toast.Notification{
			AppID:    appName,
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
