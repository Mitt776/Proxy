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
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
	"Proxy/backend/profile"
	"Proxy/backend/settings"
	"Proxy/backend/system"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// tunAutostartFlag передаётся перезапущенному с повышением прав процессу,
// чтобы он сразу поднял TUN на активном профиле.
const tunAutostartFlag = "--tun-autostart"

// App — корневая структура приложения, биндится во фронтенд.
type App struct {
	ctx      context.Context
	paths    *core.Paths
	manager  *core.Manager
	store    *profile.Store
	settings *settings.Store
	sysProxy *system.SystemProxy

	clash *core.ClashClient

	statsMu     sync.Mutex
	statsCancel context.CancelFunc

	routingMode string // config.RoutingGlobal | config.RoutingRUDirect
	blockAds    bool

	trayQuit bool // пользователь выбрал «Выход» в трее — разрешаем закрытие окна

	wasRunning   bool // для уведомлений: было ли соединение активно
	userStopping bool // пользователь сам нажал «Отключить» (не считаем обрывом)

	clashSecret string
	clashPort   int
	mixedPort   int
}

// NewApp создаёт приложение.
func NewApp() *App {
	return &App{
		clashPort:   9090,
		mixedPort:   2080,
		routingMode: config.RoutingGlobal,
		sysProxy:    system.NewSystemProxy(),
	}
}

// startup вызывается Wails при запуске: резолвим пути, поднимаем менеджер ядра,
// загружаем профили и подписываем колбэки ядра на runtime-события фронтенда.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.clashSecret = randomSecret()
	a.clash = core.NewClashClient(fmt.Sprintf("127.0.0.1:%d", a.clashPort), a.clashSecret)

	paths, err := core.ResolvePaths()
	if err != nil {
		runtime.LogErrorf(ctx, "resolve paths: %v", err)
		return
	}
	a.paths = paths

	store, err := profile.Load(paths.DataDir)
	if err != nil {
		runtime.LogErrorf(ctx, "load profiles: %v", err)
		store, _ = profile.Load(paths.DataDir) // пустое хранилище как фолбэк
	}
	a.store = store

	set, err := settings.Load(paths.DataDir)
	if err != nil {
		runtime.LogErrorf(ctx, "load settings: %v", err)
		set, _ = settings.Load(paths.DataDir)
	}
	a.settings = set
	cur := set.Get()
	a.routingMode = cur.RoutingMode
	a.blockAds = cur.BlockAds

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
			if a.wasRunning && !a.userStopping {
				trayNotify("Соединение разорвано", "Прокси отключился — трафик идёт напрямую")
			}
			a.wasRunning = false
			a.userStopping = false
			updateTraySpeed(0, 0)
		}
		if state == core.StateRunning {
			a.startStatsPoller()
			if !a.wasRunning {
				trayNotify("Подключено", "Прокси активен")
			}
			a.wasRunning = true
		}
		updateTrayMenu(string(state))
		runtime.EventsEmit(a.ctx, "core:state", map[string]string{
			"state": string(state), "reason": reason,
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
	if a.trayQuit {
		return false // пользователь явно выбрал выход
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

// onSecondInstance вызывается Wails, когда пользователь запускает ещё одну копию
// приложения. Вторую копию не плодим (её процесс завершится сам) — вместо этого
// разворачиваем и поднимаем уже работающее окно. Это чинит «кучу копий в трее».
func (a *App) onSecondInstance(_ options.SecondInstanceData) {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// --- Информация об окружении ---

// AppInfo — сводка готовности окружения.
type AppInfo struct {
	CoreVersion string `json:"coreVersion"`
	CoreFound   bool   `json:"coreFound"`
	CorePath    string `json:"corePath"`   // эффективный путь к ядру
	CoreCustom  bool   `json:"coreCustom"` // используется альтернативное ядро
	AssetsDir   string `json:"assetsDir"`
	DataDir     string `json:"dataDir"`
	State       string `json:"state"`
}

// GetAppInfo возвращает информацию об окружении и версии ядра.
func (a *App) GetAppInfo() AppInfo {
	info := AppInfo{State: string(core.StateStopped)}
	if a.paths == nil {
		return info
	}
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
		return nil, fmt.Errorf("хранилище не готово")
	}
	return a.store.AddManual(name, raw)
}

// AddSubscriptionProfile создаёт профиль-подписку по URL.
func (a *App) AddSubscriptionProfile(name, url string) (*profile.Profile, error) {
	if a.store == nil {
		return nil, fmt.Errorf("хранилище не готово")
	}
	return a.store.AddSubscription(a.ctx, name, url)
}

// RefreshProfile перезагружает подписку.
func (a *App) RefreshProfile(id string) (*profile.Profile, error) {
	return a.store.Refresh(a.ctx, id)
}

// DeleteProfile удаляет профиль.
func (a *App) DeleteProfile(id string) error {
	return a.store.Delete(id)
}

// SetActiveProfile помечает профиль активным.
func (a *App) SetActiveProfile(id string) error {
	return a.store.SetActive(id)
}

// NodeInfo — краткое описание ноды для UI.
type NodeInfo struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

// ListProfileNodes возвращает ноды профиля (для выбора в UI).
func (a *App) ListProfileNodes(id string) ([]NodeInfo, error) {
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
		return "", fmt.Errorf("хранилище не готово")
	}
	nodes, err := a.store.ResolveNodes(id)
	if err != nil {
		return "", err
	}
	var cr settings.Settings
	if a.settings != nil {
		cr = a.settings.Get()
	}
	cfg, err := config.Generate(config.Options{
		MixedPort:     a.mixedPort,
		ClashAPIPort:  a.clashPort,
		ClashSecret:   a.clashSecret,
		LogLevel:      "info",
		Nodes:         nodes,
		RoutingMode:   a.routingMode,
		BlockAds:      a.blockAds,
		RuleSetDir:    a.paths.AssetsDir,
		DirectDomains: cr.DirectDomains,
		ProxyDomains:  cr.ProxyDomains,
		BlockDomains:  cr.BlockDomains,
		CacheDBPath:   "cache.db",
	})
	if err != nil {
		return "", err
	}
	return string(cfg), nil
}

// ProfileRaw возвращает исходный ввод профиля (ссылки/JSON или тело подписки).
func (a *App) ProfileRaw(id string) (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("хранилище не готово")
	}
	p := a.store.Get(id)
	if p == nil {
		return "", fmt.Errorf("профиль не найден")
	}
	return p.Raw, nil
}

// --- Подключение ---

// Connect запускает ядро на нодах активного профиля.
// Для TUN при отсутствии прав администратора приложение перезапускается с UAC.
func (a *App) Connect(enableTUN bool) error {
	if a.store == nil || a.manager == nil {
		return fmt.Errorf("приложение не инициализировано")
	}

	// Запоминаем выбранный режим перехвата на будущие запуски.
	if a.settings != nil {
		_ = a.settings.Update(func(s *settings.Settings) { s.EnableTUN = enableTUN })
	}

	// TUN требует прав администратора для создания сетевого адаптера.
	if enableTUN && !system.IsAdmin() {
		if err := system.RelaunchElevated(tunAutostartFlag); err != nil {
			if errors.Is(err, system.ErrElevationCancelled) {
				return fmt.Errorf("для режима TUN нужны права администратора — запрос отклонён")
			}
			return fmt.Errorf("не удалось получить права администратора: %w", err)
		}
		// Управление переходит к новому (elevated) процессу — закрываем текущий.
		runtime.Quit(a.ctx)
		return nil
	}

	activeID := a.store.ActiveID()
	if activeID == "" {
		return fmt.Errorf("не выбран активный профиль")
	}
	nodes, err := a.store.ResolveNodes(activeID)
	if err != nil {
		return err
	}
	return a.startCore(nodes, enableTUN)
}

// ConnectRaw запускает ядро на нодах из произвольного ввода (ссылки/JSON/подписка).
// Пустой ввод даёт прямое соединение — удобно для проверки ядра.
func (a *App) ConnectRaw(raw string, enableTUN bool) error {
	if a.manager == nil {
		return fmt.Errorf("приложение не инициализировано")
	}
	var nodes []config.Node
	if strings.TrimSpace(raw) != "" {
		var err error
		nodes, err = config.DecodeSubscription([]byte(raw))
		if err != nil {
			return err
		}
	}
	return a.startCore(nodes, enableTUN)
}

func (a *App) startCore(nodes []config.Node, enableTUN bool) error {
	var cr settings.Settings
	if a.settings != nil {
		cr = a.settings.Get()
	}
	cfg, err := config.Generate(config.Options{
		MixedPort:     a.mixedPort,
		ClashAPIPort:  a.clashPort,
		ClashSecret:   a.clashSecret,
		LogLevel:      "info",
		EnableTUN:     enableTUN,
		Nodes:         nodes,
		RoutingMode:   a.routingMode,
		BlockAds:      a.blockAds,
		BlockQUIC:     !cr.AllowQUIC,
		RuleSetDir:    a.paths.AssetsDir,
		DirectDomains: cr.DirectDomains,
		ProxyDomains:  cr.ProxyDomains,
		BlockDomains:  cr.BlockDomains,
		GeoIPPath:     a.paths.GeoIP,
		GeoSitePath:   a.paths.GeoSite,
		CacheDBPath:   "cache.db",
	})
	if err != nil {
		return err
	}
	if err := a.manager.Start(cfg); err != nil {
		return err
	}

	// Без TUN трафик заворачивается через системный прокси на mixed-порт.
	if !enableTUN {
		if err := a.sysProxy.Set(fmt.Sprintf("127.0.0.1:%d", a.mixedPort)); err != nil {
			runtime.LogErrorf(a.ctx, "set system proxy: %v", err)
		}
	}
	return nil
}

// Disconnect снимает системный прокси и останавливает ядро.
func (a *App) Disconnect() error {
	a.userStopping = true // ручная остановка — не считаем обрывом
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

// GetLogs возвращает накопленный лог ядра.
func (a *App) GetLogs() []string {
	if a.manager == nil {
		return nil
	}
	return a.manager.Logs()
}

// --- Маршрутизация ---

// RoutingSettings — текущий режим маршрутизации для UI.
type RoutingSettings struct {
	Mode     string `json:"mode"` // global | ru-direct
	BlockAds bool   `json:"blockAds"`
}

// GetRouting возвращает текущие настройки маршрутизации.
func (a *App) GetRouting() RoutingSettings {
	return RoutingSettings{Mode: a.routingMode, BlockAds: a.blockAds}
}

// SetRouting меняет режим маршрутизации. Применяется при следующем подключении
// (смена правил требует регенерации конфига и рестарта ядра).
func (a *App) SetRouting(mode string, blockAds bool) error {
	switch mode {
	case config.RoutingGlobal, config.RoutingRUDirect:
		a.routingMode = mode
	default:
		return fmt.Errorf("неизвестный режим маршрутизации: %q", mode)
	}
	a.blockAds = blockAds
	if a.settings != nil {
		return a.settings.Update(func(s *settings.Settings) {
			s.RoutingMode = mode
			s.BlockAds = blockAds
		})
	}
	return nil
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
		return "", fmt.Errorf("приложение не инициализировано")
	}
	if a.manager.State() == core.StateRunning || a.manager.State() == core.StateStarting {
		return "", fmt.Errorf("сначала отключитесь — ядро нельзя менять на работающем подключении")
	}

	ver := ""
	if path != "" {
		v, err := coreVersion(path)
		if err != nil {
			return "", fmt.Errorf("файл не запускается как sing-box: %w", err)
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
func (a *App) ExternalIP() (IPInfo, error) {
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.mixedPort))
	hc := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	// ip-api.com — бесплатно, без ключа, по HTTP (проходит через mixed-прокси).
	resp, err := hc.Get("http://ip-api.com/json/?fields=status,message,country,countryCode,city,query")
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()

	var r struct {
		Status, Message, Country, CountryCode, City, Query string
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return IPInfo{}, err
	}
	if r.Status != "success" {
		return IPInfo{}, fmt.Errorf("гео-сервис: %s", r.Message)
	}
	return IPInfo{IP: r.Query, Country: r.Country, CountryCode: r.CountryCode, City: r.City}, nil
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
func (a *App) startSubScheduler() {
	// Первую проверку делаем вскоре после старта.
	go func() {
		time.Sleep(10 * time.Second)
		a.autoRefreshSubs()
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.autoRefreshSubs()
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

// --- Свои правила маршрутизации ---

// SetCustomRules сохраняет пользовательские домены (применяются при подключении).
func (a *App) SetCustomRules(direct, proxy, block []string) error {
	if a.settings == nil {
		return nil
	}
	return a.settings.Update(func(s *settings.Settings) {
		s.DirectDomains = cleanDomains(direct)
		s.ProxyDomains = cleanDomains(proxy)
		s.BlockDomains = cleanDomains(block)
	})
}

func cleanDomains(in []string) []string {
	var out []string
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
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
		return nil, fmt.Errorf("clash api не готов")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	proxies, err := a.clash.Proxies(ctx)
	if err != nil {
		return nil, err
	}
	sel, ok := proxies[config.ProxyTag]
	if !ok {
		return nil, fmt.Errorf("селектор %q не найден (нет активных нод?)", config.ProxyTag)
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
		return fmt.Errorf("clash api не готов")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.clash.SelectProxy(ctx, config.ProxyTag, name)
}

// TestDelay замеряет задержку одной ноды через ядро (мс).
func (a *App) TestDelay(name string) (int, error) {
	if a.clash == nil {
		return 0, fmt.Errorf("clash api не готов")
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
