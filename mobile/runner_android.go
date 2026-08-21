//go:build android

package mobile

// Ядро глазами общей логики (appcore.Runner) и журнал.
//
// На Windows эту роль играет core.Manager, гоняющий sing-box.exe отдельным
// процессом. Здесь ядро — библиотека в нашем же процессе, но поднимать её можно
// только когда жив VpnService: TUN приходит из него. Поэтому Start и Stop идут
// через Controller (Kotlin поднимает и гасит сервис), а Restart обходится своими
// силами — сервис при этом не трогаем.

import (
	"sync"

	"Proxy/backend/appcore"
	"Proxy/backend/core"
	singbox "github.com/sagernet/sing-box"
)

// maxLogLines — сколько строк журнала держим в памяти. Столько же, сколько на
// Windows (backend/core/manager.go), чтобы вкладка «Журнал» вела себя одинаково.
const maxLogLines = 2000

type runner struct {
	mu sync.Mutex

	controller Controller
	platform   Platform
	instance   *singbox.Box
	state      core.State
	logs       []string

	// pendingConfig — конфиг, с которым Kotlin просили поднять сервис. Сервис
	// стартует асинхронно и приносит его обратно в ServiceStart.
	pendingConfig []byte

	onState func(state core.State, reason string)
	onLog   func(line string)
}

func newRunner(controller Controller) *runner {
	return &runner{controller: controller, state: core.StateStopped}
}

var _ appcore.Runner = (*runner)(nil)

func (r *runner) State() core.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *runner) Logs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.logs))
	copy(out, r.logs)
	return out
}

// Check — прямой аналог `sing-box check` на Windows: конфиг проверяется до того,
// как им заменят рабочий. Платформа не нужна, проверка чисто разборочная.
func (r *runner) Check(configJSON []byte) error {
	return checkConfig(string(configJSON))
}

// Start просит Kotlin поднять VpnService. Сам box поднимется в ServiceStart —
// раньше это невозможно, TUN выдаёт сервис.
func (r *runner) Start(configJSON []byte) error {
	r.mu.Lock()
	if r.instance != nil {
		r.mu.Unlock()
		return appcore.CodedErr(appcore.ErrCoreRunning, "ядро уже запущено")
	}
	r.pendingConfig = configJSON
	r.state = core.StateStarting
	controller := r.controller
	r.mu.Unlock()

	r.emitState(core.StateStarting, "")
	if err := controller.StartTunnel(string(configJSON)); err != nil {
		r.setState(core.StateError, err.Error())
		return err
	}
	return nil
}

// Restart поднимает ядро на новом конфиге, не трогая сервис.
//
// Как и на Windows, это осознанно не Stop+Start: промежуточное «остановлено» тут
// сняло бы уведомление и сбросило таймер сессии, а на десктопе ещё и системный
// прокси. VpnService.Builder.establish() при живом туннеле просто заменяет его,
// поэтому пользователь разрыва не видит.
func (r *runner) Restart(configJSON []byte) error {
	r.mu.Lock()
	platform := r.platform
	r.mu.Unlock()
	if platform == nil {
		return appcore.CodedErr(appcore.ErrCoreStopped, "туннель не запущен")
	}

	r.closeInstance()
	if err := r.openInstance(configJSON, platform); err != nil {
		// Ядро не поднялось на новом конфиге — туннеля больше нет, и делать вид,
		// что всё хорошо, нельзя: гасим сервис и честно показываем ошибку.
		r.setState(core.StateError, err.Error())
		r.controller.StopTunnel()
		return err
	}
	return nil
}

// Stop гасит ядро и сервис. Идемпотентен: остановка уже остановленного — не
// ошибка, иначе гонка между кнопкой «Отключить» и смертью сервиса давала бы
// ложную ошибку в интерфейсе.
func (r *runner) Stop() error {
	r.mu.Lock()
	controller := r.controller
	running := r.instance != nil || r.state != core.StateStopped
	r.mu.Unlock()
	if !running {
		return nil
	}
	controller.StopTunnel()
	return nil
}

// --- со стороны сервиса ---

// serviceStart зовётся из TunnelService, когда VpnService уже готов выдать TUN.
func (r *runner) serviceStart(configJSON []byte, platform Platform) error {
	r.mu.Lock()
	if len(configJSON) == 0 {
		configJSON = r.pendingConfig
	}
	r.platform = platform
	r.mu.Unlock()

	if len(configJSON) == 0 {
		err := appcore.CodedErr(appcore.ErrNotReady, "конфиг не подготовлен")
		r.setState(core.StateError, err.Error())
		return err
	}
	if err := r.openInstance(configJSON, platform); err != nil {
		r.setState(core.StateError, err.Error())
		return err
	}
	return nil
}

// serviceStop зовётся из TunnelService при его остановке — в том числе когда
// туннель погасила система (отзыв разрешения, always-on другого приложения).
func (r *runner) serviceStop() {
	r.closeInstance()
	r.mu.Lock()
	r.platform = nil
	r.pendingConfig = nil
	r.mu.Unlock()
	r.setState(core.StateStopped, "")
}

func (r *runner) openInstance(configJSON []byte, platform Platform) error {
	instance, err := newInstance(string(configJSON), platform, r.appendLog)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.instance = instance
	r.mu.Unlock()
	r.setState(core.StateRunning, "")
	return nil
}

func (r *runner) closeInstance() {
	r.mu.Lock()
	instance := r.instance
	r.instance = nil
	r.mu.Unlock()
	if instance != nil {
		_ = instance.Close()
	}
}

func (r *runner) setState(state core.State, reason string) {
	r.mu.Lock()
	if r.state == state {
		r.mu.Unlock()
		return
	}
	r.state = state
	r.mu.Unlock()
	r.emitState(state, reason)
}

func (r *runner) emitState(state core.State, reason string) {
	if r.onState != nil {
		r.onState(state, reason)
	}
}

func (r *runner) appendLog(line string) {
	r.mu.Lock()
	r.logs = append(r.logs, line)
	if len(r.logs) > maxLogLines {
		r.logs = r.logs[len(r.logs)-maxLogLines:]
	}
	r.mu.Unlock()
	if r.onLog != nil {
		r.onLog(line)
	}
}
