package appcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"Proxy/backend/core"
	"Proxy/backend/profile"
	"Proxy/backend/rules"
	"Proxy/backend/settings"
)

// Host — то, что обязана дать платформа. Всё, чего нет одновременно и в Wails, и в
// Android, живёт здесь, а не в Core.
type Host interface {
	// Emit шлёт событие во фронтенд: на Windows это runtime.EventsEmit,
	// на Android — вызов через мост в WebView.
	Emit(name string, payload any)
	// Logf пишет в журнал платформы (не в журнал ядра).
	Logf(format string, args ...any)
	// ProfilesChanged — список профилей или активный профиль изменились. На
	// Windows тут перерисовывается меню трея; на Android делать нечего.
	ProfilesChanged()
	// OnStats — свежая скорость (байт/с) для платформенных индикаторов: подпись
	// в трее на Windows, уведомление на Android.
	OnStats(downSpeed, upSpeed int64)
	// DefaultLang — язык системы, когда пользователь его не выбирал.
	DefaultLang() string
}

// Runner — ядро глазами общей логики. На Windows это core.Manager, гоняющий
// sing-box.exe отдельным процессом; на Android — обёртка над библиотекой в этом же
// процессе. Набор методов подобран так, чтобы *core.Manager подходил как есть.
type Runner interface {
	State() core.State
	Check(configJSON []byte) error
	Start(configJSON []byte) error
	Restart(configJSON []byte) error
	Stop() error
	Logs() []string
}

// Core — переносимая часть приложения. Всё, что фронтенд может спросить и что не
// требует ни Wails, ни WinAPI.
type Core struct {
	host   Host
	runner Runner

	paths    *core.Paths
	profiles *profile.Store
	settings *settings.Store
	rules    *rules.Store
	clash    *core.ClashClient

	// Секрет Clash API генерируется заново на каждый запуск: порт слушает
	// localhost, но на Android до него дотягивается любое приложение.
	clashSecret string
	clashPort   int
	mixedPort   int

	// connectedAt — момент перехода ядра в running (unix-миллисекунды), 0 = не
	// подключены. Фронтенд считает время сессии как now - since, а не инкрементом:
	// в скрытом окне таймеры троттлятся и счётчик уплывает.
	connectedAt atomic.Int64

	ctx       context.Context
	ctxCancel context.CancelFunc

	statsMu     sync.Mutex
	statsCancel context.CancelFunc
}

// Options — параметры создания Core.
type Options struct {
	Host  Host
	Paths *core.Paths
	// ClashPort и MixedPort фиксированы в конфиге ядра; нули заменяются
	// значениями по умолчанию (9090 и 2080).
	ClashPort int
	MixedPort int
}

// New создаёт ядро приложения. Хранилища ещё не прочитаны — это делает Load.
func New(opts Options) *Core {
	if opts.ClashPort == 0 {
		opts.ClashPort = 9090
	}
	if opts.MixedPort == 0 {
		opts.MixedPort = 2080
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Core{
		host:        opts.Host,
		paths:       opts.Paths,
		clashPort:   opts.ClashPort,
		mixedPort:   opts.MixedPort,
		clashSecret: randomSecret(),
		ctx:         ctx,
		ctxCancel:   cancel,
	}
	c.clash = core.NewClashClient(fmt.Sprintf("127.0.0.1:%d", c.clashPort), c.clashSecret)
	return c
}

// LoadIssue — файл состояния не прочитался. Хранилище при этом всё равно рабочее,
// а повреждённый файл отложен в *.bad; платформа решает, как сообщить об этом
// пользователю.
type LoadIssue struct {
	Kind string // profiles | settings | routing
	Err  error
}

// Load читает профили, настройки и правила. **Всегда** оставляет Core рабочим:
// nil-хранилище роняло бы половину API, поэтому ошибки отдаются списком, а не
// прерывают загрузку.
func (c *Core) Load() []LoadIssue {
	var issues []LoadIssue

	store, err := profile.Load(c.paths.DataDir)
	if err != nil {
		issues = append(issues, LoadIssue{Kind: "profiles", Err: err})
	}
	c.profiles = store

	set, err := settings.Load(c.paths.DataDir)
	if err != nil {
		issues = append(issues, LoadIssue{Kind: "settings", Err: err})
	}
	c.settings = set
	cur := set.Get()

	rs, err := rules.Load(c.paths.DataDir)
	if err != nil {
		issues = append(issues, LoadIssue{Kind: "routing", Err: err})
	}
	// routing.json ещё нет — обновление с версии до 1.2.0 или чистая установка.
	// Собираем список из старых настроек, чтобы поведение не поменялось под
	// пользователем.
	if !rs.Exists() {
		migrated := rules.Migrate(cur.RoutingMode, cur.BlockAds,
			cur.DirectDomains, cur.ProxyDomains, cur.BlockDomains)
		if err := rs.Init(migrated); err != nil {
			issues = append(issues, LoadIssue{Kind: "routing", Err: err})
		}
	}
	c.rules = rs

	return issues
}

// SetRunner подключает ядро. Отдельно от New: на Windows менеджер создаётся после
// резолва путей, на Android — после старта сервиса.
func (c *Core) SetRunner(r Runner) { c.runner = r }

// Close останавливает фоновые горутины (планировщик подписок, поллер статистики).
func (c *Core) Close() {
	c.StopStatsPoller()
	c.ctxCancel()
}

// --- доступ к внутренностям для платформенного слоя ---

// Paths — каталоги ассетов и данных.
func (c *Core) Paths() *core.Paths { return c.paths }

// Profiles — хранилище профилей.
func (c *Core) Profiles() *profile.Store { return c.profiles }

// Settings — хранилище настроек.
func (c *Core) Settings() *settings.Store { return c.settings }

// Rules — хранилище правил маршрутизации.
func (c *Core) Rules() *rules.Store { return c.rules }

// Clash — клиент Clash API работающего ядро.
func (c *Core) Clash() *core.ClashClient { return c.clash }

// ClashSecret — секрет Clash API текущего запуска.
func (c *Core) ClashSecret() string { return c.clashSecret }

// Context — контекст жизни приложения; отменяется в Close.
func (c *Core) Context() context.Context { return c.ctx }

// ProxyAddr — адрес локального mixed-прокси. Один источник правды: по нему же на
// Windows распознаётся системный прокси, оставшийся от аварийного завершения.
func (c *Core) ProxyAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.mixedPort)
}

// ListSetDir — каталог со списками .lst, сконвертированными в source-наборы.
// Лежит в данных, а не в ассетах: содержимое качается и переписывается, а каталог
// ассетов может быть только для чтения.
func (c *Core) ListSetDir() string {
	if c.paths == nil {
		return ""
	}
	return filepath.Join(c.paths.DataDir, "rulesets")
}

// running — работает ли ядро прямо сейчас.
func (c *Core) running() bool {
	return c.runner != nil && c.runner.State() == core.StateRunning
}

// State — текущее состояние ядра.
func (c *Core) State() string {
	if c.runner == nil {
		return string(core.StateStopped)
	}
	return string(c.runner.State())
}

// Status — состояние соединения одним куском: то же, что прилетает в событии
// core:state, но по запросу.
type Status struct {
	State string `json:"state"`
	Since int64  `json:"since"` // начало сессии, unix ms (0 = не подключены)
}

// GetStatus нужен при первичной загрузке UI. Событие core:state приходит только на
// смену состояния, поэтому окно, показанное через час после старта, без этого
// метода не знало бы, с какого момента считать сессию, и показывало бы нули на
// живом подключении.
func (c *Core) GetStatus() Status {
	return Status{State: c.State(), Since: c.connectedAt.Load()}
}

// ConnectedAt — момент начала сессии (unix ms), 0 если не подключены.
func (c *Core) ConnectedAt() int64 { return c.connectedAt.Load() }

// MarkConnected ставит метку начала сессии, если её ещё нет. Именно CompareAndSwap:
// перезапуск ядра ради правил не должен сбрасывать таймер подключения.
func (c *Core) MarkConnected() { c.connectedAt.CompareAndSwap(0, time.Now().UnixMilli()) }

// MarkDisconnected сбрасывает метку сессии.
func (c *Core) MarkDisconnected() { c.connectedAt.Store(0) }

// GetLogs возвращает накопленный журнал ядра.
func (c *Core) GetLogs() []string {
	if c.runner == nil {
		return nil
	}
	return c.runner.Logs()
}

func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "proxy-secret"
	}
	return hex.EncodeToString(b)
}
