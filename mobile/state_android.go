//go:build android

package mobile

// Состояние приложения на Android — то, чем на Windows является App в app.go.
// Вся логика при этом общая (backend/appcore); здесь только платформенная обвязка:
// события в WebView, подключение/отключение и язык системы.

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"Proxy/backend/appcore"
	"Proxy/backend/config"
	"Proxy/backend/core"
)

type application struct {
	core    *appcore.Core
	runner  *runner
	sink    EventSink
	sysLang string

	wasRunning atomic.Bool
}

var (
	appMu sync.Mutex
	app   *application
)

// --- appcore.Host ---

// Emit шлёт событие во фронтенд. Полезная нагрузка едет JSON-строкой: gomobile
// умеет возить только простые типы, а на той стороне её всё равно скармливают
// JSON.parse — ровно как это делает Wails на Windows.
func (a *application) Emit(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte("null")
	}
	a.sink.OnEvent(name, string(data))
}

// Logf кладёт сообщение приложения в тот же поток, что и журнал ядра: другого
// места, где пользователь мог бы его увидеть, на телефоне нет.
func (a *application) Logf(format string, args ...any) {
	a.core.LogLine(sprintf(format, args...))
}

// ProfilesChanged — на Windows тут перерисовывается меню трея; на Android трея нет.
func (a *application) ProfilesChanged() {}

// OnStats — скорость для уведомления сервиса. Отдельным событием, чтобы Kotlin не
// разбирал core:stats ради двух чисел.
func (a *application) OnStats(downSpeed, upSpeed int64) {
	a.sink.OnSpeed(downSpeed, upSpeed)
}

// DefaultLang — язык системы, определённый Kotlin при старте.
func (a *application) DefaultLang() string { return a.sysLang }

// --- состояние ядра ---

func (a *application) onCoreState(state core.State, reason string) {
	switch state {
	case core.StateStopped, core.StateError:
		a.core.StopStatsPoller()
		a.core.MarkDisconnected()
		a.wasRunning.Store(false)
		a.sink.OnSpeed(0, 0)
	case core.StateRunning:
		a.core.StartStatsPoller()
		// Отсчёт сессии начинаем только на переходе в running: перезапуск ядра
		// ради правил не должен сбрасывать таймер подключения.
		a.core.MarkConnected()
		a.wasRunning.Store(true)
	}
	a.sink.OnState(string(state), reason)
	a.Emit("core:state", map[string]any{
		"state": string(state), "reason": reason, "since": a.core.ConnectedAt(),
	})
}

func (a *application) onCoreLog(line string) { a.Emit("core:log", line) }

// --- подключение ---

// Connect собирает конфиг активного профиля и просит Kotlin поднять туннель.
//
// enableTUN тут нет: на Android перехват всегда полный, системного прокси не
// существует. Параметр остаётся в конфиге ради общего генератора.
func (a *application) Connect() error {
	nodes, err := a.core.ActiveNodes()
	if err != nil {
		return err
	}
	cfg, err := config.Generate(a.core.ConfigOptions(nodes, true))
	if err != nil {
		return err
	}
	// Проверяем конфиг до старта: иначе о неподдерживаемом поле мы узнаём по
	// мгновенно умершему туннелю и невнятной ошибке в журнале.
	if err := a.runner.Check(cfg); err != nil {
		return appcore.CodedErrf(appcore.ErrCoreCheck, "%w", err)
	}
	if err := a.runner.Start(cfg); err != nil {
		return err
	}
	a.core.RememberTUN(true)
	return nil
}

// Disconnect гасит туннель.
func (a *application) Disconnect() error { return a.runner.Stop() }

// appInfo — сводка окружения для раздела «О программе».
type appInfo struct {
	AppVersion  string `json:"appVersion"`
	CoreVersion string `json:"coreVersion"`
	CoreFound   bool   `json:"coreFound"`
	DataDir     string `json:"dataDir"`
	State       string `json:"state"`
	Since       int64  `json:"since"`
	Lang        string `json:"lang"`
	// Platform отличает сборки во фронтенде: часть разделов на телефоне не нужна.
	Platform string `json:"platform"`
}

func (a *application) appInfo() appInfo {
	return appInfo{
		AppVersion:  appcore.AppVersion,
		CoreVersion: coreVersion(),
		CoreFound:   true, // ядро вшито в APK, искать его негде
		DataDir:     basePath(),
		State:       a.core.State(),
		Since:       a.core.ConnectedAt(),
		Lang:        a.core.CurrentLang(),
		Platform:    "android",
	}
}
