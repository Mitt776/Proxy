package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
// при выпуске бампится здесь и в wails.json (блок `info.productVersion`, откуда
// её берут свойства exe-файла).
const AppVersion = "2.0.1"

// tunAutostartFlag передаётся перезапущенному с повышением прав процессу,
// чтобы он сразу поднял TUN на активном профиле.
const tunAutostartFlag = "--tun-autostart"

// waitPidFlag (`--wait-pid=1234`) сообщает перезапущенному процессу PID предка,
// которого нужно дождаться перед стартом GUI. Elevated- и обычный процесс делят
// одну WebView2 user data folder, но не могут держать её одновременно из-за
// разного integrity level — второй получит окно без содержимого.
const waitPidFlag = "--wait-pid"

// App — корневая структура приложения, биндится во фронтенд.
type App struct {
	ctx      context.Context
	paths    *core.Paths
	manager  *core.Manager
	store    *profile.Store
	settings *settings.Store
	rules    *rules.Store
	sysProxy *system.SystemProxy

	clash *core.ClashClient

	statsMu     sync.Mutex
	statsCancel context.CancelFunc

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

	// connectedAt — момент перехода ядра в running (unix-миллисекунды), 0 = не
	// подключены. Отсюда фронтенд считает время сессии как now - since. Именно
	// метку времени, а не счётчик: в скрытом окне (свёрнуты в трей) WebView2
	// троттлит таймеры, и любой инкрементный счётчик уплыл бы на минуты.
	connectedAt atomic.Int64

	clashSecret string
	clashPort   int
	mixedPort   int
}

// NewApp создаёт приложение.
func NewApp() *App {
	return &App{
		clashPort: 9090,
		mixedPort: 2080,
		sysProxy:  system.NewSystemProxy(),
	}
}

// startup вызывается Wails при запуске: резолвим пути, поднимаем менеджер ядра,
// загружаем профили и подписываем колбэки ядра на runtime-события фронтенда.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.uiReady.Store(true)
	a.clashSecret = randomSecret()
	a.clash = core.NewClashClient(fmt.Sprintf("127.0.0.1:%d", a.clashPort), a.clashSecret)

	paths, err := core.ResolvePaths()
	if err != nil {
		runtime.LogErrorf(ctx, "resolve paths: %v", err)
		return
	}
	a.paths = paths

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
	store, err := profile.Load(paths.DataDir)
	if err != nil {
		runtime.LogErrorf(ctx, "load profiles: %v", err)
		trayNotify("Профили не загружены", err.Error())
	}
	a.store = store

	set, err := settings.Load(paths.DataDir)
	if err != nil {
		runtime.LogErrorf(ctx, "load settings: %v", err)
		trayNotify("Настройки не загружены", err.Error())
	}
	a.settings = set
	cur := set.Get()

	// Язык интерфейса нужен раньше трея: меню строится сразу на нужном языке.
	trayLang.Store(a.currentLang())

	// Правила маршрутизации. Если routing.json ещё нет (обновление с версии до
	// 1.2.0 или чистая установка) — собираем список из старых настроек, чтобы
	// поведение не поменялось под пользователем.
	rs, err := rules.Load(paths.DataDir)
	if err != nil {
		runtime.LogErrorf(ctx, "load routing: %v", err)
		trayNotify("Правила не загружены", err.Error())
	}
	if !rs.Exists() {
		migrated := rules.Migrate(cur.RoutingMode, cur.BlockAds,
			cur.DirectDomains, cur.ProxyDomains, cur.BlockDomains)
		if err := rs.Init(migrated); err != nil {
			runtime.LogErrorf(ctx, "init routing: %v", err)
		}
	}
	a.rules = rs

	m := core.NewManager(paths)
	m.OnLog = func(line string) { runtime.EventsEmit(a.ctx, "core:log", line) }
	m.OnState = func(state core.State, reason string) {
		// Если ядро остановилось (в т.ч. авария) — обязательно снимаем системный
		// прокси, иначе у пользователя «пропадёт» интернет.
		if state == core.StateStopped || state == core.StateError {
			_ = a.sysProxy.Clear()
			a.stopStatsPoller()
			// Уведомление об обрыве только если соединение было активно и его
			// разорвал не сам пользователь.
			if a.wasRunning.Load() && !a.userStopping.Load() {
				tt := trayT()
				trayNotify(tt.NotifyLostTitle, tt.NotifyLostBody)
			}
			a.wasRunning.Store(false)
			a.userStopping.Store(false)
			a.connectedAt.Store(0)
			updateTraySpeed(0, 0)
		}
		if state == core.StateRunning {
			a.startStatsPoller()
			// Отсчёт сессии начинаем только на переходе в running: при перезапуске
			// ядра ради правил (Manager.Restart) состояние сюда не приходит, и
			// таймер честно продолжает идти с момента подключения.
			a.connectedAt.CompareAndSwap(0, time.Now().UnixMilli())
			if !a.wasRunning.Swap(true) {
				tt := trayT()
				trayNotify(tt.NotifyUpTitle, tt.NotifyUpBody)
			}
		}
		updateTrayMenu(string(state))
		runtime.EventsEmit(a.ctx, "core:state", map[string]any{
			"state": string(state), "reason": reason, "since": a.connectedAt.Load(),
		})
	}
	a.manager = m

	// wintun.dll в рабочий каталог — чтобы TUN работал и с альтернативным ядром.
	paths.EnsureWintun()

	// Применяем сохранённое альтернативное ядро (если найдено), иначе — встроенное.
	if resolved := a.resolveCorePath(cur.CorePath); resolved != "" {
		if _, err := coreVersion(resolved); err == nil {
			m.SetBinaryPath(resolved)
		} else {
			runtime.LogErrorf(ctx, "альтернативное ядро %q недоступно, откат на встроенное: %v", resolved, err)
		}
	}

	// Иконка в трее (собственный цикл сообщений в отдельной горутине).
	go a.runTray()

	// Фоновое автообновление подписок по расписанию.
	a.startSubScheduler()

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

// shutdown вызывается при закрытии окна — гарантированно гасим ядро и чистим систему.
func (a *App) shutdown(ctx context.Context) {
	_ = a.sysProxy.Clear()
	if a.manager != nil {
		_ = a.manager.Stop()
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
	if a.settings != nil {
		minimize = a.settings.Get().MinimizeToTray
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
		Lang:        a.currentLang(),
		StartHidden: hasFlag(system.MinimizedFlag),
	}
	if a.paths == nil {
		return info
	}
	info.Since = a.connectedAt.Load()
	info.AssetsDir = a.paths.AssetsDir
	info.DataDir = a.paths.DataDir
	info.State = string(a.manager.State())

	corePath := a.manager.BinaryPath()
	info.CorePath = corePath
	info.CoreCustom = a.settings != nil && a.settings.Get().CorePath != ""
	if ver, err := coreVersion(corePath); err == nil {
		info.CoreVersion = ver
		info.CoreFound = true
	}
	return info
}

// --- Профили ---

// ListProfiles возвращает все профили.
func (a *App) ListProfiles() []*profile.Profile {
	if a.store == nil {
		return nil
	}
	return a.store.List()
}

// GetActiveProfileID возвращает id активного профиля.
func (a *App) GetActiveProfileID() string {
	if a.store == nil {
		return ""
	}
	return a.store.ActiveID()
}

// AddManualProfile создаёт ручной профиль из ссылок/JSON.
func (a *App) AddManualProfile(name, raw string) (*profile.Profile, error) {
	if a.store == nil {
		return nil, codedErr(ErrNotReady, "хранилище не готово")
	}
	p, err := a.store.AddManual(name, raw)
	a.rebuildTrayProfiles()
	return p, err
}

// AddSubscriptionProfile создаёт профиль-подписку по URL.
func (a *App) AddSubscriptionProfile(name, url string) (*profile.Profile, error) {
	if a.store == nil {
		return nil, codedErr(ErrNotReady, "хранилище не готово")
	}
	p, err := a.store.AddSubscription(a.ctx, name, url)
	a.rebuildTrayProfiles()
	return p, err
}

// RefreshProfile перезагружает подписку.
func (a *App) RefreshProfile(id string) (*profile.Profile, error) {
	if a.store == nil {
		return nil, codedErr(ErrNotReady, "хранилище не готово")
	}
	return a.store.Refresh(a.ctx, id)
}

// DeleteProfile удаляет профиль.
func (a *App) DeleteProfile(id string) error {
	if a.store == nil {
		return codedErr(ErrNotReady, "хранилище не готово")
	}
	err := a.store.Delete(id)
	a.rebuildTrayProfiles()
	return err
}

// SetActiveProfile помечает профиль активным.
//
// На живом соединении смена профиля обязана менять и трафик: раньше метод только
// записывал ID, ядро продолжало работать на прежних нодах, и «Сменить профиль»
// молча ничего не делало до переподключения. Пересборка идёт тем же путём, что и
// правки правил (`applyRouting` → `Manager.Restart`), поэтому системный прокси с
// активного соединения не слетает. Не принятый ядром профиль откатывается.
func (a *App) SetActiveProfile(id string) error {
	if a.store == nil {
		return codedErr(ErrNotReady, "хранилище не готово")
	}
	prev := a.store.ActiveID()
	if err := a.store.SetActive(id); err != nil {
		return err
	}
	a.rebuildTrayProfiles()

	if err := a.applyRouting(); err != nil {
		_ = a.store.SetActive(prev)
		a.rebuildTrayProfiles()
		return err
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "profiles:changed", nil)
	}
	return nil
}

// NodeInfo — краткое описание ноды для UI.
type NodeInfo struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

// ListProfileNodes возвращает ноды профиля (для выбора в UI).
func (a *App) ListProfileNodes(id string) ([]NodeInfo, error) {
	if a.store == nil {
		return nil, codedErr(ErrNotReady, "хранилище не готово")
	}
	nodes, err := a.store.ResolveNodes(id)
	if err != nil {
		return nil, err
	}
	return nodeInfos(nodes), nil
}

// ProfileConfigJSON возвращает готовый config.json sing-box для профиля
// (в mixed-режиме, с текущими настройками маршрутизации) — для копирования/шаринга.
func (a *App) ProfileConfigJSON(id string) (string, error) {
	if a.store == nil {
		return "", codedErr(ErrNotReady, "хранилище не готово")
	}
	nodes, err := a.store.ResolveNodes(id)
	if err != nil {
		return "", err
	}
	cfg, err := config.Generate(a.configOptions(nodes, false))
	if err != nil {
		return "", err
	}
	return string(cfg), nil
}

// ProfileRaw возвращает исходный ввод профиля (ссылки/JSON или тело подписки).
func (a *App) ProfileRaw(id string) (string, error) {
	if a.store == nil {
		return "", codedErr(ErrNotReady, "хранилище не готово")
	}
	p := a.store.Get(id)
	if p == nil {
		return "", codedErr(ErrProfileNotFound, "профиль не найден")
	}
	return p.Raw, nil
}

// --- Подключение ---

// Connect запускает ядро на нодах активного профиля.
// Для TUN при отсутствии прав администратора приложение перезапускается с UAC.
func (a *App) Connect(enableTUN bool) error {
	if a.store == nil || a.manager == nil {
		return codedErr(ErrNotReady, "приложение не инициализировано")
	}

	// TUN требует прав администратора для создания сетевого адаптера.
	if enableTUN && !system.IsAdmin() {
		// Свой PID — чтобы новый процесс дождался нашей смерти и только потом
		// поднимал WebView2 (общую user data folder нельзя делить с elevated).
		waitFlag := fmt.Sprintf("%s=%d", waitPidFlag, os.Getpid())
		if err := system.RelaunchElevated(tunAutostartFlag, waitFlag); err != nil {
			if errors.Is(err, system.ErrElevationCancelled) {
				return codedErr(ErrTUNRights, "для режима TUN нужны права администратора — запрос отклонён")
			}
			return codedErrf(ErrElevate, "не удалось получить права администратора: %w", err)
		}
		// Режим запоминаем только теперь: откажи пользователь в UAC — и следующий
		// запуск (автозапуск, кнопка в трее) снова полез бы за правами.
		a.rememberTUN(enableTUN)
		// Управление переходит к новому (elevated) процессу — закрываем текущий.
		// Флаг обязателен: без него beforeClose спрячет окно в трей вместо выхода.
		a.relaunching.Store(true)
		runtime.Quit(a.ctx)
		return nil
	}

	activeID := a.store.ActiveID()
	if activeID == "" {
		return codedErr(ErrNoProfile, "не выбран активный профиль")
	}
	nodes, err := a.store.ResolveNodes(activeID)
	if err != nil {
		return err
	}
	if err := a.startCore(nodes, enableTUN); err != nil {
		return err
	}
	a.rememberTUN(enableTUN)
	return nil
}

// rememberTUN запоминает выбранный режим перехвата на будущие запуски. Пишем
// его только после того, как подключение действительно состоялось: сохранённый
// «TUN» при неудачном старте заставлял бы автозапуск и трей каждый раз лезть за
// правами администратора.
func (a *App) rememberTUN(enableTUN bool) {
	if a.settings == nil {
		return
	}
	_ = a.settings.Update(func(s *settings.Settings) { s.EnableTUN = enableTUN })
}

// proxyAddr — адрес локального mixed-прокси, который мы прописываем в системные
// настройки Windows. Один источник правды: по нему же на старте распознаётся
// прокси, оставшийся от аварийно завершённого запуска.
func (a *App) proxyAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", a.mixedPort)
}

// configOptions собирает параметры генерации конфига из текущего состояния
// приложения: настройки, правила маршрутизации и пути к ассетам.
func (a *App) configOptions(nodes []config.Node, enableTUN bool) config.Options {
	var cr settings.Settings
	if a.settings != nil {
		cr = a.settings.Get()
	}
	var routing rules.Config
	if a.rules != nil {
		routing = a.rules.Get()
	}
	opts := config.Options{
		MixedPort:    a.mixedPort,
		ClashAPIPort: a.clashPort,
		ClashSecret:  a.clashSecret,
		LogLevel:     normalizeLogLevel(cr.LogLevel),
		EnableTUN:    enableTUN,
		Nodes:        nodes,
		Routing:      routing,
		Mode:         cr.Mode,
		BlockQUIC:    !cr.AllowQUIC,
		CacheDBPath:  "cache.db",
	}
	if a.paths != nil {
		opts.RuleSetDir = a.paths.AssetsDir
		opts.ListSetDir = a.listSetDir()
		opts.GeoIPPath = a.paths.GeoIP
		opts.GeoSitePath = a.paths.GeoSite
	}
	return opts
}

// listSetDir — каталог со списками .lst, сконвертированными в source-наборы.
// Лежит в данных, а не в ассетах: содержимое качается и переписывается, а
// каталог ассетов может быть только для чтения.
func (a *App) listSetDir() string {
	if a.paths == nil {
		return ""
	}
	return filepath.Join(a.paths.DataDir, "rulesets")
}

func (a *App) startCore(nodes []config.Node, enableTUN bool) error {
	cfg, err := config.Generate(a.configOptions(nodes, enableTUN))
	if err != nil {
		return err
	}
	// Проверяем конфиг ядром до запуска: иначе о неподдерживаемом поле мы узнаём
	// по мгновенно умершему процессу и невнятной ошибке в логе.
	if err := a.manager.Check(cfg); err != nil {
		return codedErrf(ErrCoreCheck, "%w", err)
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
func (a *App) IsAdmin() bool {
	return system.IsAdmin()
}

func hasFlag(flag string) bool {
	for _, a := range os.Args[1:] {
		if a == flag {
			return true
		}
	}
	return false
}

// GetState возвращает текущее состояние ядра.
func (a *App) GetState() string {
	if a.manager == nil {
		return string(core.StateStopped)
	}
	return string(a.manager.State())
}

// Status — состояние соединения одним куском: то же, что прилетает в событии
// core:state, но по запросу.
type Status struct {
	State string `json:"state"`
	Since int64  `json:"since"` // начало сессии, unix ms (0 = не подключены)
}

// GetStatus нужен при первичной загрузке UI. Событие core:state приходит только
// на смену состояния, поэтому окно, показанное из трея через час после старта (или
// поднятое с --tun-autostart), без этого метода не знало бы, с какого момента
// считать время сессии, и показывало бы нули на живом подключении.
func (a *App) GetStatus() Status {
	st := Status{State: string(core.StateStopped)}
	if a.manager != nil {
		st.State = string(a.manager.State())
	}
	st.Since = a.connectedAt.Load()
	return st
}

// --- Язык интерфейса ---

// currentLang возвращает выбранный язык, а если пользователь его не выбирал —
// определённый по локали Windows.
func (a *App) currentLang() string {
	if a.settings != nil {
		if l := normalizeLang(a.settings.Get().Lang); l != "" {
			return l
		}
	}
	return system.DefaultLang()
}

// normalizeLang приводит язык к поддерживаемому значению; "" = не задан.
func normalizeLang(lang string) string {
	switch strings.ToLower(lang) {
	case "ru":
		return "ru"
	case "en":
		return "en"
	}
	return ""
}

// GetLanguage возвращает текущий язык интерфейса (ru|en).
func (a *App) GetLanguage() string { return a.currentLang() }

// SetLanguage запоминает язык и сразу перетитровывает меню трея — оно живёт вне
// фронтенда и само по себе на смену языка не отреагирует.
func (a *App) SetLanguage(lang string) error {
	l := normalizeLang(lang)
	if l == "" {
		return codedErrf(ErrLangUnknown, "неизвестный язык: %q", lang)
	}
	if a.settings != nil {
		if err := a.settings.Update(func(s *settings.Settings) { s.Lang = l }); err != nil {
			return err
		}
	}
	applyTrayLang(l)
	if a.manager != nil {
		updateTrayMenu(string(a.manager.State())) // тултип тоже на новом языке
	}
	return nil
}

// GetLogs возвращает накопленный лог ядра.
func (a *App) GetLogs() []string {
	if a.manager == nil {
		return nil
	}
	return a.manager.Logs()
}

// ListProcesses возвращает запущенные процессы с иконками — для пикера при
// создании правила «этот процесс мимо прокси».
func (a *App) ListProcesses() ([]system.ProcessInfo, error) {
	return system.ListProcesses()
}

// --- Режим маршрутизации ---

// GetMode возвращает текущий режим: Rule (работают правила), Global (всё через
// прокси) или Direct (всё напрямую). У живого ядра спрашиваем сам ядро — режим
// мог поменяться из другого клиента Clash API.
func (a *App) GetMode() string {
	if a.manager != nil && a.manager.State() == core.StateRunning && a.clash != nil {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
		defer cancel()
		if mode, err := a.clash.Mode(ctx); err == nil && mode != "" {
			return mode
		}
	}
	if a.settings != nil {
		if m := a.settings.Get().Mode; m != "" {
			return m
		}
	}
	return config.ModeRule
}

// SetMode переключает режим. На работающем ядре это делается через Clash API
// (PATCH /configs) — мгновенно и без разрыва соединений; перезапуск не нужен.
func (a *App) SetMode(mode string) error {
	switch mode {
	case config.ModeRule, config.ModeGlobal, config.ModeDirect:
	default:
		return codedErrf(ErrModeUnknown, "неизвестный режим: %q", mode)
	}
	// Сначала живому ядру, потом на диск: откажи ядро — и сохранённый режим
	// разошёлся бы с тем, по которому реально идёт трафик.
	if a.manager != nil && a.manager.State() == core.StateRunning && a.clash != nil {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		if err := a.clash.SetMode(ctx, mode); err != nil {
			return err
		}
	}
	if a.settings != nil {
		if err := a.settings.Update(func(s *settings.Settings) { s.Mode = mode }); err != nil {
			return err
		}
	}
	runtime.EventsEmit(a.ctx, "core:mode", mode)
	return nil
}

// --- Маршрутизация: правила и группы ---

// GetRouting возвращает весь список правил и групп для UI.
func (a *App) GetRouting() rules.Config {
	if a.rules == nil {
		return rules.Default()
	}
	return a.rules.Get()
}

// withRouting меняет правила и сразу применяет их к живому ядру. Если ядро
// отвергло новый конфиг (или не смогло перезапуститься), правки откатываются:
// иначе на диске осталось бы правило, о котором ядро не знает, — UI показывает
// одно, трафик идёт по-другому, и разобраться в этом пользователю нечем.
func (a *App) withRouting(mutate func() error) error {
	if a.rules == nil {
		return codedErr(ErrNotReady, "правила не готовы")
	}
	prev := a.rules.Get() // глубокая копия — состояние до правки
	if err := mutate(); err != nil {
		return err
	}
	if err := a.applyRouting(); err != nil {
		if rerr := a.rules.Replace(prev); rerr != nil {
			return fmt.Errorf("%w (откатить правила не удалось: %v)", err, rerr)
		}
		return err
	}
	return nil
}

// SetRouting заменяет список правил целиком (drag-n-drop в UI) и применяет
// изменения к работающему ядру.
func (a *App) SetRouting(cfg rules.Config) error {
	return a.withRouting(func() error { return a.rules.Replace(cfg) })
}

// AddRule добавляет правило в конец списка и возвращает его ID.
func (a *App) AddRule(r rules.Rule) (string, error) {
	var id string
	err := a.withRouting(func() error {
		var e error
		id, e = a.rules.AddRule(r)
		return e
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateRule сохраняет изменённое правило.
func (a *App) UpdateRule(r rules.Rule) error {
	return a.withRouting(func() error { return a.rules.UpdateRule(r) })
}

// DeleteRule удаляет правило (встроенные удалить нельзя — только выключить).
func (a *App) DeleteRule(id string) error {
	return a.withRouting(func() error { return a.rules.DeleteRule(id) })
}

// MoveRule переставляет правило на позицию index — порядок задаёт приоритет.
func (a *App) MoveRule(id string, index int) error {
	return a.withRouting(func() error { return a.rules.MoveRule(id, index) })
}

// SetRuleEnabled включает или выключает правило.
func (a *App) SetRuleEnabled(id string, enabled bool) error {
	return a.withRouting(func() error { return a.rules.SetRuleEnabled(id, enabled) })
}

// SetRoutingFinal задаёт судьбу трафика, не попавшего ни под одно правило:
// rules.ActionProxy (всё через прокси) или rules.ActionDirect (сплит-туннель).
func (a *App) SetRoutingFinal(final string) error {
	return a.withRouting(func() error {
		return a.rules.Update(func(c *rules.Config) { c.Final = final })
	})
}

// AddGroup создаёт группу нод и возвращает её ID.
func (a *App) AddGroup(g rules.Group) (string, error) {
	var id string
	err := a.withRouting(func() error {
		var e error
		id, e = a.rules.AddGroup(g)
		return e
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateGroup сохраняет изменённую группу (переименование тянет за собой
// ссылки правил).
func (a *App) UpdateGroup(g rules.Group) error {
	return a.withRouting(func() error { return a.rules.UpdateGroup(g) })
}

// DeleteGroup удаляет группу; ссылавшиеся на неё правила переходят на основной
// селектор.
func (a *App) DeleteGroup(id string) error {
	return a.withRouting(func() error { return a.rules.DeleteGroup(id) })
}

// applyRouting пересобирает конфиг с новыми правилами и перезапускает ядро,
// если оно работает. Перезапуск идёт через Manager.Restart — он не показывает
// промежуточное «остановлено», иначе с активного соединения слетел бы системный
// прокси и пользователь на секунду остался бы без интернета.
func (a *App) applyRouting() error {
	if a.manager == nil || a.manager.State() != core.StateRunning {
		return nil // применится при следующем подключении
	}
	if a.store == nil {
		return nil
	}
	activeID := a.store.ActiveID()
	if activeID == "" {
		return nil
	}
	nodes, err := a.store.ResolveNodes(activeID)
	if err != nil {
		return err
	}
	enableTUN := a.settings != nil && a.settings.Get().EnableTUN
	cfg, err := config.Generate(a.configOptions(nodes, enableTUN))
	if err != nil {
		return err
	}
	// Битый конфиг не должен ронять живое соединение: проверяем до перезапуска.
	if err := a.manager.Check(cfg); err != nil {
		return codedErrf(ErrCoreCheck, "%w", err)
	}
	return a.manager.Restart(cfg)
}

// GetSettings возвращает сохранённые настройки (для инициализации UI).
func (a *App) GetSettings() settings.Settings {
	if a.settings == nil {
		return settings.Defaults()
	}
	return a.settings.Get()
}

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
		return "", codedErr(ErrNotReady, "приложение не инициализировано")
	}
	if a.manager.State() == core.StateRunning || a.manager.State() == core.StateStarting {
		return "", codedErr(ErrCoreRunning, "сначала отключитесь — ядро нельзя менять на работающем подключении")
	}

	ver := ""
	if path != "" {
		v, err := coreVersion(path)
		if err != nil {
			return "", codedErrf(ErrCoreInvalid, "файл не запускается как sing-box: %w", err)
		}
		ver = v
	}
	a.manager.SetBinaryPath(path)
	if a.settings != nil {
		_ = a.settings.Update(func(s *settings.Settings) { s.CorePath = path })
	}
	return ver, nil
}

// ResetCorePath возвращает встроенное ядро.
func (a *App) ResetCorePath() error {
	_, err := a.SetCorePath("")
	return err
}

// SetBlockQUIC включает/выключает резку QUIC в TUN (применяется при подключении).
func (a *App) SetBlockQUIC(block bool) error {
	if a.settings == nil {
		return nil
	}
	return a.settings.Update(func(s *settings.Settings) { s.AllowQUIC = !block })
}

// SetMinimizeToTray включает/выключает сворачивание в трей при закрытии окна.
func (a *App) SetMinimizeToTray(enable bool) error {
	if a.settings == nil {
		return nil
	}
	return a.settings.Update(func(s *settings.Settings) { s.MinimizeToTray = enable })
}

// GetAutostart сообщает, включён ли автозапуск (по факту записи в реестре).
func (a *App) GetAutostart() bool {
	return system.AutostartEnabled()
}

// SetAutostart включает/выключает автозапуск и сохраняет выбор.
func (a *App) SetAutostart(enable bool) error {
	if err := system.SetAutostart(enable); err != nil {
		return err
	}
	if a.settings != nil {
		_ = a.settings.Update(func(s *settings.Settings) { s.Autostart = enable })
	}
	return nil
}

// --- Внешний IP и гео (через прокси) ---

// IPInfo — внешний IP и его гео, полученные через прокси.
type IPInfo struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
}

// ExternalIP запрашивает внешний IP и страну ЧЕРЕЗ локальный mixed-прокси —
// то есть показывает, откуда «видит» пользователя интернет после подключения.
// Пробуем несколько сервисов: если один недоступен/лимитит — берём следующий.
func (a *App) ExternalIP() (IPInfo, error) {
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.mixedPort))
	hc := &http.Client{
		Timeout:   7 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	for _, provider := range []func(*http.Client) (IPInfo, error){geoIPAPI, geoIPWhoIs, geoIPInfo} {
		if info, err := provider(hc); err == nil && info.IP != "" {
			return info, nil
		}
	}
	return IPInfo{}, codedErr(ErrIPUnknown, "не удалось определить внешний IP")
}

// geoIPAPI — ip-api.com (полное имя страны + код + город; HTTP, но без ключа).
func geoIPAPI(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("http://ip-api.com/json/?fields=status,country,countryCode,city,query")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct{ Status, Country, CountryCode, City, Query string }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if r.Status != "success" {
		return IPInfo{}, fmt.Errorf("ip-api: %s", r.Status)
	}
	return IPInfo{IP: r.Query, Country: r.Country, CountryCode: r.CountryCode, City: r.City}, nil
}

// geoIPWhoIs — ipwho.is (HTTPS, полное имя + код + город).
func geoIPWhoIs(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("https://ipwho.is/")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct {
		IP          string `json:"ip"`
		Success     bool   `json:"success"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if !r.Success {
		return IPInfo{}, fmt.Errorf("ipwho.is: fail")
	}
	return IPInfo{IP: r.IP, Country: r.Country, CountryCode: r.CountryCode, City: r.City}, nil
}

// geoIPInfo — ipinfo.io (HTTPS; страна только кодом, но как надёжный фолбэк).
func geoIPInfo(hc *http.Client) (IPInfo, error) {
	resp, err := hc.Get("https://ipinfo.io/json")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var r struct{ IP, Country, City string }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if r.IP == "" {
		return IPInfo{}, fmt.Errorf("ipinfo: пусто")
	}
	// Country тут — двухбуквенный код; полного имени нет.
	return IPInfo{IP: r.IP, Country: "", CountryCode: r.Country, City: r.City}, nil
}

// --- Автообновление подписок ---

// SetSubUpdateHours задаёт интервал автообновления подписок (0 — выключить).
func (a *App) SetSubUpdateHours(hours int) error {
	if a.settings == nil {
		return nil
	}
	if hours < 0 {
		hours = 0
	}
	return a.settings.Update(func(s *settings.Settings) { s.SubUpdateHours = hours })
}

// startSubScheduler раз в 30 минут проверяет подписки и обновляет те, что старше
// заданного интервала. Так смена интервала не требует перезапуска планировщика.
// Заодно тем же тиком докачиваются текстовые списки правил (.lst).
func (a *App) startSubScheduler() {
	// Первую проверку делаем вскоре после старта.
	go func() {
		time.Sleep(10 * time.Second)
		a.autoRefreshSubs()
		a.refreshListSets()
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.autoRefreshSubs()
			a.refreshListSets()
		}
	}()
}

func (a *App) autoRefreshSubs() {
	if a.settings == nil || a.store == nil {
		return
	}
	hours := a.settings.Get().SubUpdateHours
	if hours <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	changed := false
	for _, p := range a.store.List() {
		if p.Kind == "subscription" && p.SubURL != "" && p.UpdatedAt.Before(cutoff) {
			if _, err := a.store.Refresh(a.ctx, p.ID); err == nil {
				changed = true
			}
		}
	}
	if changed {
		runtime.EventsEmit(a.ctx, "profiles:changed", nil)
	}
}

// --- Clash API: ноды, задержка, переключение, статистика ---

// ProxiesView — состояние selector'а для UI: выбранная нода и все варианты.
type ProxiesView struct {
	Selector string      `json:"selector"`
	Now      string      `json:"now"`
	Nodes    []ProxyNode `json:"nodes"`
}

// ProxyNode — одна нода/группа в списке selector'а с последней задержкой.
type ProxyNode struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Delay int    `json:"delay"` // мс; 0 — не измерялось/недоступно
}

// GetProxies возвращает selector "proxy", выбранную ноду и её варианты с задержками.
func (a *App) GetProxies() (*ProxiesView, error) {
	if a.clash == nil {
		return nil, codedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	proxies, err := a.clash.Proxies(ctx)
	if err != nil {
		return nil, err
	}
	sel, ok := proxies[config.ProxyTag]
	if !ok {
		return nil, codedErrf(ErrNoSelector, "селектор %q не найден (нет активных нод?)", config.ProxyTag)
	}
	view := &ProxiesView{Selector: config.ProxyTag, Now: sel.Now}
	for _, name := range sel.All {
		p := proxies[name]
		view.Nodes = append(view.Nodes, ProxyNode{
			Name:  name,
			Type:  p.Type,
			Delay: p.LastDelay(),
		})
	}
	return view, nil
}

// SelectNode переключает selector на выбранную ноду (без рестарта ядра).
func (a *App) SelectNode(name string) error {
	if a.clash == nil {
		return codedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.clash.SelectProxy(ctx, config.ProxyTag, name)
}

// TestDelay замеряет задержку одной ноды через ядро (мс).
func (a *App) TestDelay(name string) (int, error) {
	if a.clash == nil {
		return 0, codedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return a.clash.Delay(ctx, name, "", 5000)
}

// startStatsPoller раз в секунду опрашивает /connections и шлёт "core:stats"
// (скорость вверх/вниз, суммарные байты, число соединений). Скорость считаем
// как дельту суммарных счётчиков между опросами.
func (a *App) startStatsPoller() {
	a.statsMu.Lock()
	if a.statsCancel != nil {
		a.statsMu.Unlock()
		return // уже запущен
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.statsCancel = cancel
	a.statsMu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		var lastDown, lastUp int64
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t, err := a.clash.Connections(ctx)
				if err != nil {
					continue
				}
				var downSpeed, upSpeed int64
				if !first {
					downSpeed = nonNeg(t.DownloadTotal - lastDown)
					upSpeed = nonNeg(t.UploadTotal - lastUp)
				}
				lastDown, lastUp = t.DownloadTotal, t.UploadTotal
				first = false
				updateTraySpeed(downSpeed, upSpeed)
				runtime.EventsEmit(a.ctx, "core:stats", map[string]interface{}{
					"downSpeed":   downSpeed,
					"upSpeed":     upSpeed,
					"downTotal":   t.DownloadTotal,
					"upTotal":     t.UploadTotal,
					"connections": len(t.Connections),
				})
			}
		}
	}()
}

// stopStatsPoller останавливает поллер статистики (если запущен).
func (a *App) stopStatsPoller() {
	a.statsMu.Lock()
	if a.statsCancel != nil {
		a.statsCancel()
		a.statsCancel = nil
	}
	a.statsMu.Unlock()
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// --- вспомогательное ---

func nodeInfos(nodes []config.Node) []NodeInfo {
	out := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		info := NodeInfo{Tag: n.Tag}
		var meta struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(n.Outbound, &meta)
		info.Type = meta.Type
		out = append(out, info)
	}
	return out
}

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

func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "proxy-secret"
	}
	return hex.EncodeToString(b)
}
