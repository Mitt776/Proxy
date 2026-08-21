package main

// App — оболочка Wails поверх backend/appcore.
//
// Вся переносимая логика (профили, маршрутизация, настройки, Clash API, подписки)
// живёт в appcore и одинаково обслуживает Windows и Android. Здесь остаётся только
// то, чего на Android нет или что там устроено иначе: системный прокси, UAC ради
// TUN, трей, автозапуск и выбор файла ядра.
//
// Публичные методы App = API для фронтенда: Wails биндит их в JS. Поэтому
// переносимые методы продублированы тут однострочными делегатами — так у
// фронтенда сохраняется ровно та же поверхность, что и раньше.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"Proxy/backend/appcore"
	"Proxy/backend/config"
	"Proxy/backend/core"
	"Proxy/backend/profile"
	"Proxy/backend/rules"
	"Proxy/backend/settings"
	"Proxy/backend/system"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// appName — имя приложения в заголовке окна, трее и уведомлениях Windows.
const appName = "MitM"

// AppVersion — версия приложения. Единственный источник правды для UI и трея;
// при выпуске бампится здесь, в wails.json (блок `info.productVersion`, откуда её
// берут свойства exe-файла) и в android/app/build.gradle.kts.
const AppVersion = "2.0.2"

// tunAutostartFlag передаётся перезапущенному с повышением прав процессу,
// чтобы он сразу поднял TUN на активном профиле.
const tunAutostartFlag = "--tun-autostart"

// waitPidFlag (`--wait-pid=1234`) сообщает перезапущенному процессу PID предка,
// которого нужно дождаться перед стартом GUI. Elevated- и обычный процесс делят
// одну WebView2 user data folder, но не могут держать её одновременно из-за
// разного integrity level — второй получит окно без содержимого.
const waitPidFlag = "--wait-pid"

// codedErr/codedErrf — коды ошибок для двуязычного интерфейса, см. appcore/errcodes.go.
var (
	codedErr  = appcore.CodedErr
	codedErrf = appcore.CodedErrf
)

// App — корневая структура приложения, биндится во фронтенд.
type App struct {
	ctx      context.Context
	core     *appcore.Core
	paths    *core.Paths
	manager  *core.Manager
	sysProxy *system.SystemProxy

	// Флаги ниже пишутся и читаются из разных потоков (цикл сообщений трея,
	// горутина наблюдения за ядром, поток окна Wails), поэтому атомарные.
	trayQuit atomic.Bool // пользователь выбрал «Выход» в трее — разрешаем закрытие окна
	// relaunching — идёт передача управления процессу, перезапущенному с UAC.
	// Как и trayQuit, отключает перехват закрытия в beforeClose: иначе окно лишь
	// спрячется в трей, старый процесс останется жить, и его WebView2 не пустит
	// элевированный процесс к общей user data folder — окно будет пустым.
	relaunching atomic.Bool

	// uiReady — startup отработал и a.ctx годен для вызовов runtime. Нужен
	// горутине, которая слушает запуски второй копии: она стартует раньше окна.
	uiReady atomic.Bool

	wasRunning   atomic.Bool // для уведомлений: было ли соединение активно
	userStopping atomic.Bool // пользователь сам нажал «Отключить» (не считаем обрывом)
}

// NewApp создаёт приложение.
func NewApp() *App {
	return &App{sysProxy: system.NewSystemProxy()}
}

// --- appcore.Host: то, что переносимое ядро просит у платформы ---

// Emit шлёт событие во фронтенд.
func (a *App) Emit(name string, payload any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, payload)
	}
}

// Logf пишет в журнал Wails.
func (a *App) Logf(format string, args ...any) {
	if a.ctx != nil {
		runtime.LogErrorf(a.ctx, format, args...)
	}
}

// ProfilesChanged перерисовывает список профилей в меню трея.
func (a *App) ProfilesChanged() { a.rebuildTrayProfiles() }

// OnStats обновляет скорость в подписи трея.
func (a *App) OnStats(downSpeed, upSpeed int64) { updateTraySpeed(downSpeed, upSpeed) }

// DefaultLang — язык по локали Windows.
func (a *App) DefaultLang() string { return system.DefaultLang() }

// --- жизненный цикл окна ---

// startup вызывается Wails при запуске: резолвим пути, поднимаем менеджер ядра,
// загружаем состояние и подписываем колбэки ядра на runtime-события фронтенда.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.uiReady.Store(true)

	paths, err := core.ResolvePaths()
	if err != nil {
		runtime.LogErrorf(ctx, "resolve paths: %v", err)
		return
	}
	a.paths = paths

	a.core = appcore.New(appcore.Options{Host: a, Paths: paths})

	// Прошлый запуск мог умереть, не сняв системный прокси (краш, taskkill,
	// выключение питания). В реестре остался наш адрес, а слушать его уже некому
	// — для пользователя это выглядит как «пропал интернет». Снимаем.
	if cleared, cerr := a.sysProxy.ClearStale(a.proxyAddr()); cerr != nil {
		runtime.LogErrorf(ctx, "clear stale proxy: %v", cerr)
	} else if cleared {
		runtime.LogInfo(ctx, "снят системный прокси, оставшийся от прошлого запуска")
	}

	// Хранилища возвращаются рабочими даже при ошибке чтения: без профилей или
	// правил приложение живёт, а вот с nil-хранилищем половина API падает.
	// Повреждённый файл при этом отложен в *.bad — сообщаем об этом пользователю.
	for _, issue := range a.core.Load() {
		runtime.LogErrorf(ctx, "load %s: %v", issue.Kind, issue.Err)
		trayNotify(loadIssueTitle(issue.Kind), issue.Err.Error())
	}

	// Язык интерфейса нужен раньше трея: меню строится сразу на нужном языке.
	trayLang.Store(a.core.CurrentLang())

	m := core.NewManager(paths)
	m.OnLog = func(line string) { runtime.EventsEmit(a.ctx, "core:log", line) }
	m.OnState = a.onCoreState
	a.manager = m
	a.core.SetRunner(m)

	// wintun.dll в рабочий каталог — чтобы TUN работал и с альтернативным ядром.
	paths.EnsureWintun()

	// Применяем сохранённое альтернативное ядро (если найдено), иначе — встроенное.
	if resolved := a.resolveCorePath(a.core.GetSettings().CorePath); resolved != "" {
		if _, err := coreVersion(resolved); err == nil {
			m.SetBinaryPath(resolved)
		} else {
			runtime.LogErrorf(ctx, "альтернативное ядро %q недоступно, откат на встроенное: %v", resolved, err)
		}
	}

	// Иконка в трее (собственный цикл сообщений в отдельной горутине).
	go a.runTray()

	// Фоновое автообновление подписок по расписанию.
	a.core.StartSubScheduler()

	// Перезапущены с повышением прав ради TUN — сразу поднимаем активный профиль.
	if hasFlag(tunAutostartFlag) {
		go func() {
			time.Sleep(400 * time.Millisecond) // дать фронту подписаться на события
			if err := a.Connect(true); err != nil {
				runtime.LogErrorf(a.ctx, "tun autostart: %v", err)
			}
		}()
	}
}

// loadIssueTitle — заголовок уведомления о непрочитанном файле состояния.
func loadIssueTitle(kind string) string {
	switch kind {
	case "profiles":
		return "Профили не загружены"
	case "settings":
		return "Настройки не загружены"
	default:
		return "Правила не загружены"
	}
}

// onCoreState — реакция на смену состояния ядра. Всё, что здесь делается, кроме
// самого события во фронтенд, специфично для Windows: системный прокси, трей и
// уведомления.
func (a *App) onCoreState(state core.State, reason string) {
	// Если ядро остановилось (в т.ч. авария) — обязательно снимаем системный
	// прокси, иначе у пользователя «пропадёт» интернет.
	if state == core.StateStopped || state == core.StateError {
		_ = a.sysProxy.Clear()
		a.core.StopStatsPoller()
		// Уведомление об обрыве только если соединение было активно и его
		// разорвал не сам пользователь.
		if a.wasRunning.Load() && !a.userStopping.Load() {
			tt := trayT()
			trayNotify(tt.NotifyLostTitle, tt.NotifyLostBody)
		}
		a.wasRunning.Store(false)
		a.userStopping.Store(false)
		a.core.MarkDisconnected()
		updateTraySpeed(0, 0)
	}
	if state == core.StateRunning {
		a.core.StartStatsPoller()
		// Отсчёт сессии начинаем только на переходе в running: при перезапуске
		// ядра ради правил (Manager.Restart) состояние сюда не приходит, и таймер
		// честно продолжает идти с момента подключения.
		a.core.MarkConnected()
		if !a.wasRunning.Swap(true) {
			tt := trayT()
			trayNotify(tt.NotifyUpTitle, tt.NotifyUpBody)
		}
	}
	updateTrayMenu(string(state))
	runtime.EventsEmit(a.ctx, "core:state", map[string]any{
		"state": string(state), "reason": reason, "since": a.core.ConnectedAt(),
	})
}

// shutdown вызывается при закрытии окна — гарантированно гасим ядро и чистим систему.
func (a *App) shutdown(ctx context.Context) {
	_ = a.sysProxy.Clear()
	if a.manager != nil {
		_ = a.manager.Stop()
	}
	if a.core != nil {
		a.core.Close()
	}
	stopTray()
}

// beforeClose перехватывает закрытие окна: по умолчанию прячем приложение в трей
// (ядро продолжает работать). Реальное завершение — только через «Выход» в трее.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.trayQuit.Load() || a.relaunching.Load() {
		return false // явный выход из трея либо передача управления elevated-процессу
	}
	minimize := true
	if a.core != nil {
		minimize = a.core.GetSettings().MinimizeToTray
	}
	// Прячем в трей только если он реально поднялся — иначе окно не вернуть.
	if minimize && TrayReady() {
		runtime.WindowHide(ctx)
		return true // отменяем закрытие — остаёмся в трее
	}
	return false
}

// onSecondInstance вызывается, когда пользователь запускает ещё одну копию
// приложения (см. system.ClaimSingleInstance). Вторую копию не плодим — её
// процесс завершается сам, а мы разворачиваем и поднимаем уже работающее окно.
//
// Зовётся из чужой горутины и может успеть раньше startup: до готовности ctx
// показывать нечего, а runtime с nil-контекстом уронил бы приложение.
func (a *App) onSecondInstance() {
	if !a.uiReady.Load() {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// --- Информация об окружении ---

// AppInfo — сводка готовности окружения.
type AppInfo struct {
	AppVersion  string `json:"appVersion"`
	CoreVersion string `json:"coreVersion"`
	CoreFound   bool   `json:"coreFound"`
	CorePath    string `json:"corePath"`   // эффективный путь к ядру
	CoreCustom  bool   `json:"coreCustom"` // используется альтернативное ядро
	AssetsDir   string `json:"assetsDir"`
	DataDir     string `json:"dataDir"`
	State       string `json:"state"`
	Since       int64  `json:"since"`       // начало сессии, unix ms (0 = не подключены)
	IsAdmin     bool   `json:"isAdmin"`     // запущены с правами администратора
	Lang        string `json:"lang"`        // язык интерфейса: ru|en
	StartHidden bool   `json:"startHidden"` // автозапуск в трей — заставку не крутим
}

// GetAppInfo возвращает информацию об окружении и версии ядра.
func (a *App) GetAppInfo() AppInfo {
	info := AppInfo{
		AppVersion:  AppVersion,
		State:       string(core.StateStopped),
		IsAdmin:     system.IsAdmin(),
		StartHidden: hasFlag(system.MinimizedFlag),
	}
	if a.core == nil {
		return info
	}
	info.Lang = a.core.CurrentLang()
	info.Since = a.core.ConnectedAt()
	info.AssetsDir = a.paths.AssetsDir
	info.DataDir = a.paths.DataDir
	info.State = a.core.State()

	corePath := a.manager.BinaryPath()
	info.CorePath = corePath
	info.CoreCustom = a.core.GetSettings().CorePath != ""
	if ver, err := coreVersion(corePath); err == nil {
		info.CoreVersion = ver
		info.CoreFound = true
	}
	return info
}

// --- Подключение ---

// Connect запускает ядро на нодах активного профиля.
// Для TUN при отсутствии прав администратора приложение перезапускается с UAC.
func (a *App) Connect(enableTUN bool) error {
	if a.core == nil || a.manager == nil {
		return codedErr(appcore.ErrNotReady, "приложение не инициализировано")
	}

	// TUN требует прав администратора для создания сетевого адаптера.
	if enableTUN && !system.IsAdmin() {
		// Свой PID — чтобы новый процесс дождался нашей смерти и только потом
		// поднимал WebView2 (общую user data folder нельзя делить с elevated).
		waitFlag := fmt.Sprintf("%s=%d", waitPidFlag, os.Getpid())
		if err := system.RelaunchElevated(tunAutostartFlag, waitFlag); err != nil {
			if errors.Is(err, system.ErrElevationCancelled) {
				return codedErr(appcore.ErrTUNRights, "для режима TUN нужны права администратора — запрос отклонён")
			}
			return codedErrf(appcore.ErrElevate, "не удалось получить права администратора: %w", err)
		}
		// Режим запоминаем только теперь: откажи пользователь в UAC — и следующий
		// запуск (автозапуск, кнопка в трее) снова полез бы за правами.
		a.core.RememberTUN(enableTUN)
		// Управление переходит к новому (elevated) процессу — закрываем текущий.
		// Флаг обязателен: без него beforeClose спрячет окно в трей вместо выхода.
		a.relaunching.Store(true)
		runtime.Quit(a.ctx)
		return nil
	}

	nodes, err := a.core.ActiveNodes()
	if err != nil {
		return err
	}
	if err := a.startCore(nodes, enableTUN); err != nil {
		return err
	}
	a.core.RememberTUN(enableTUN)
	return nil
}

// proxyAddr — адрес локального mixed-прокси, который мы прописываем в системные
// настройки Windows. Один источник правды: по нему же на старте распознаётся
// прокси, оставшийся от аварийно завершённого запуска.
func (a *App) proxyAddr() string {
	if a.core != nil {
		return a.core.ProxyAddr()
	}
	return "127.0.0.1:2080"
}

func (a *App) startCore(nodes []config.Node, enableTUN bool) error {
	cfg, err := config.Generate(a.core.ConfigOptions(nodes, enableTUN))
	if err != nil {
		return err
	}
	// Проверяем конфиг ядром до запуска: иначе о неподдерживаемом поле мы узнаём
	// по мгновенно умершему процессу и невнятной ошибке в логе.
	if err := a.manager.Check(cfg); err != nil {
		return codedErrf(appcore.ErrCoreCheck, "%w", err)
	}
	if err := a.manager.Start(cfg); err != nil {
		return err
	}

	// Без TUN трафик заворачивается через системный прокси на mixed-порт.
	if !enableTUN {
		if err := a.sysProxy.Set(a.proxyAddr()); err != nil {
			runtime.LogErrorf(a.ctx, "set system proxy: %v", err)
		}
	}
	return nil
}

// Disconnect снимает системный прокси и останавливает ядро.
func (a *App) Disconnect() error {
	a.userStopping.Store(true) // ручная остановка — не считаем обрывом
	_ = a.sysProxy.Clear()
	if a.manager == nil {
		return nil
	}
	return a.manager.Stop()
}

// IsAdmin сообщает фронтенду, запущены ли мы с правами администратора.
func (a *App) IsAdmin() bool { return system.IsAdmin() }

func hasFlag(flag string) bool {
	for _, arg := range os.Args[1:] {
		if arg == flag {
			return true
		}
	}
	return false
}

// --- Язык интерфейса ---

// GetLanguage возвращает текущий язык интерфейса (ru|en).
func (a *App) GetLanguage() string { return a.core.CurrentLang() }

// SetLanguage запоминает язык и сразу перетитровывает меню трея — оно живёт вне
// фронтенда и само по себе на смену языка не отреагирует.
func (a *App) SetLanguage(lang string) error {
	l, err := a.core.SetLanguage(lang)
	if err != nil {
		return err
	}
	applyTrayLang(l)
	if a.manager != nil {
		updateTrayMenu(string(a.manager.State())) // тултип тоже на новом языке
	}
	return nil
}

// --- Ядро: выбор бинарника (только Windows) ---

// resolveCorePath делает выбор ядра портативным. Сохранённый путь может быть
// абсолютным (напр. с флэшки D:\… → на другом ПК E:\…). Если файла по этому пути
// нет — ищем файл с тем же именем в каталоге ассетов рядом с exe. Так ядро,
// вшитое в архив (assets\sing-box-xhttp.exe), переезжает вместе с приложением.
func (a *App) resolveCorePath(stored string) string {
	if stored == "" {
		return ""
	}
	if fileExists(stored) {
		return stored
	}
	if a.paths != nil {
		alt := filepath.Join(a.paths.AssetsDir, filepath.Base(stored))
		if fileExists(alt) {
			return alt
		}
	}
	return "" // не найдено — откат на встроенное ядро
}

// fileExists — есть ли обычный файл по пути.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// PickCoreFile открывает диалог выбора sing-box.exe и применяет его как ядро.
// Возвращает версию выбранного ядра (для показа в UI).
func (a *App) PickCoreFile() (string, error) {
	defaultDir := ""
	if a.paths != nil {
		defaultDir = a.paths.AssetsDir // тут лежит вшитый sing-box-xhttp.exe
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Выберите sing-box.exe (альтернативное ядро)",
		DefaultDirectory: defaultDir,
		Filters: []runtime.FileFilter{
			{DisplayName: "sing-box.exe", Pattern: "*.exe"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // пользователь отменил
	}
	return a.SetCorePath(path)
}

// SetCorePath проверяет и назначает альтернативное ядро (пустой путь — сброс на
// встроенное). Возвращает строку версии выбранного ядра.
func (a *App) SetCorePath(path string) (string, error) {
	if a.manager == nil {
		return "", codedErr(appcore.ErrNotReady, "приложение не инициализировано")
	}
	if a.manager.State() == core.StateRunning || a.manager.State() == core.StateStarting {
		return "", codedErr(appcore.ErrCoreRunning, "сначала отключитесь — ядро нельзя менять на работающем подключении")
	}

	ver := ""
	if path != "" {
		v, err := coreVersion(path)
		if err != nil {
			return "", codedErrf(appcore.ErrCoreInvalid, "файл не запускается как sing-box: %w", err)
		}
		ver = v
	}
	a.manager.SetBinaryPath(path)
	if s := a.core.Settings(); s != nil {
		_ = s.Update(func(st *settings.Settings) { st.CorePath = path })
	}
	return ver, nil
}

// ResetCorePath возвращает встроенное ядро.
func (a *App) ResetCorePath() error {
	_, err := a.SetCorePath("")
	return err
}

// --- Windows-специфичные настройки ---

// SetMinimizeToTray включает/выключает сворачивание в трей при закрытии окна.
func (a *App) SetMinimizeToTray(enable bool) error {
	s := a.core.Settings()
	if s == nil {
		return nil
	}
	return s.Update(func(st *settings.Settings) { st.MinimizeToTray = enable })
}

// GetAutostart сообщает, включён ли автозапуск (по факту записи в реестре).
func (a *App) GetAutostart() bool { return system.AutostartEnabled() }

// SetAutostart включает/выключает автозапуск и сохраняет выбор.
func (a *App) SetAutostart(enable bool) error {
	if err := system.SetAutostart(enable); err != nil {
		return err
	}
	if s := a.core.Settings(); s != nil {
		_ = s.Update(func(st *settings.Settings) { st.Autostart = enable })
	}
	return nil
}

// ListProcesses возвращает запущенные процессы с иконками — для пикера при
// создании правила «этот процесс мимо прокси».
func (a *App) ListProcesses() ([]system.ProcessInfo, error) { return system.ListProcesses() }

// --- Делегаты в appcore: тот же API для фронтенда ---

// GetState возвращает текущее состояние ядра.
func (a *App) GetState() string { return a.core.State() }

// GetStatus возвращает состояние соединения одним куском.
func (a *App) GetStatus() appcore.Status { return a.core.GetStatus() }

// GetLogs возвращает накопленный лог ядра.
func (a *App) GetLogs() []string { return a.core.GetLogs() }

// ListProfiles возвращает все профили.
func (a *App) ListProfiles() []*profile.Profile { return a.core.ListProfiles() }

// GetActiveProfileID возвращает id активного профиля.
func (a *App) GetActiveProfileID() string { return a.core.GetActiveProfileID() }

// AddManualProfile создаёт ручной профиль из ссылок/JSON.
func (a *App) AddManualProfile(name, raw string) (*profile.Profile, error) {
	return a.core.AddManualProfile(name, raw)
}

// AddSubscriptionProfile создаёт профиль-подписку по URL.
func (a *App) AddSubscriptionProfile(name, url string) (*profile.Profile, error) {
	return a.core.AddSubscriptionProfile(name, url)
}

// RefreshProfile перезагружает подписку.
func (a *App) RefreshProfile(id string) (*profile.Profile, error) { return a.core.RefreshProfile(id) }

// DeleteProfile удаляет профиль.
func (a *App) DeleteProfile(id string) error { return a.core.DeleteProfile(id) }

// SetActiveProfile помечает профиль активным и пересобирает живое соединение.
func (a *App) SetActiveProfile(id string) error { return a.core.SetActiveProfile(id) }

// ListProfileNodes возвращает ноды профиля (для выбора в UI).
func (a *App) ListProfileNodes(id string) ([]appcore.NodeInfo, error) {
	return a.core.ListProfileNodes(id)
}

// ProfileConfigJSON возвращает готовый config.json sing-box для профиля.
func (a *App) ProfileConfigJSON(id string) (string, error) { return a.core.ProfileConfigJSON(id) }

// ProfileRaw возвращает исходный ввод профиля.
func (a *App) ProfileRaw(id string) (string, error) { return a.core.ProfileRaw(id) }

// ProfileQR возвращает QR-код профиля как data-URL.
func (a *App) ProfileQR(id string) (string, error) { return a.core.ProfileQR(id) }

// GetMode возвращает текущий режим маршрутизации Clash API.
func (a *App) GetMode() string { return a.core.GetMode() }

// SetMode переключает режим маршрутизации на живом ядре.
func (a *App) SetMode(mode string) error { return a.core.SetMode(mode) }

// GetRouting возвращает весь список правил и групп для UI.
func (a *App) GetRouting() rules.Config { return a.core.GetRouting() }

// SetRouting заменяет список правил целиком.
func (a *App) SetRouting(cfg rules.Config) error { return a.core.SetRouting(cfg) }

// AddRule добавляет правило и возвращает его ID.
func (a *App) AddRule(r rules.Rule) (string, error) { return a.core.AddRule(r) }

// UpdateRule сохраняет изменённое правило.
func (a *App) UpdateRule(r rules.Rule) error { return a.core.UpdateRule(r) }

// DeleteRule удаляет правило.
func (a *App) DeleteRule(id string) error { return a.core.DeleteRule(id) }

// MoveRule переставляет правило на позицию index.
func (a *App) MoveRule(id string, index int) error { return a.core.MoveRule(id, index) }

// SetRuleEnabled включает или выключает правило.
func (a *App) SetRuleEnabled(id string, enabled bool) error {
	return a.core.SetRuleEnabled(id, enabled)
}

// SetRoutingFinal задаёт судьбу трафика, не попавшего ни под одно правило.
func (a *App) SetRoutingFinal(final string) error { return a.core.SetRoutingFinal(final) }

// AddGroup создаёт группу нод и возвращает её ID.
func (a *App) AddGroup(g rules.Group) (string, error) { return a.core.AddGroup(g) }

// UpdateGroup сохраняет изменённую группу.
func (a *App) UpdateGroup(g rules.Group) error { return a.core.UpdateGroup(g) }

// DeleteGroup удаляет группу.
func (a *App) DeleteGroup(id string) error { return a.core.DeleteGroup(id) }

// GetSettings возвращает сохранённые настройки.
func (a *App) GetSettings() settings.Settings { return a.core.GetSettings() }

// SetBlockQUIC включает/выключает резку QUIC в TUN.
func (a *App) SetBlockQUIC(block bool) error { return a.core.SetBlockQUIC(block) }

// SetSubUpdateHours задаёт интервал автообновления подписок (0 — выключить).
func (a *App) SetSubUpdateHours(hours int) error { return a.core.SetSubUpdateHours(hours) }

// ExternalIP запрашивает внешний IP и страну через локальный прокси.
func (a *App) ExternalIP() (appcore.IPInfo, error) { return a.core.ExternalIP() }

// GetProxies возвращает селектор нод с задержками.
func (a *App) GetProxies() (*appcore.ProxiesView, error) { return a.core.GetProxies() }

// SelectNode переключает selector на выбранную ноду.
func (a *App) SelectNode(name string) error { return a.core.SelectNode(name) }

// TestDelay замеряет задержку одной ноды через ядро (мс).
func (a *App) TestDelay(name string) (int, error) { return a.core.TestDelay(name) }

// --- вспомогательное ---

func coreVersion(binPath string) (string, error) {
	out, err := runHidden(binPath, "version")
	if err != nil {
		return "", err
	}
	line := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	return strings.TrimSpace(line), nil
}

func runHidden(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	hideCmdWindow(cmd)
	out, err := cmd.Output()
	return string(out), err
}
