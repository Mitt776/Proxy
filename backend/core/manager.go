// Package core управляет жизненным циклом внешнего процесса sing-box.
package core

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// State — состояние ядра.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateError    State = "error"
)

const maxLogLines = 2000

// Manager запускает и останавливает sing-box.exe и собирает его вывод.
// Он не зависит от Wails: наверх отдаёт события через колбэки OnLog/OnState,
// которые app.go проксирует в runtime-события фронтенда.
type Manager struct {
	paths *Paths

	mu         sync.Mutex
	cmd        *exec.Cmd
	state      State
	logs       []string
	logFile    *os.File // дубликат вывода ядра на диск (box.log) для диагностики
	binaryPath string   // альтернативный sing-box.exe (пусто = paths.SingBox)

	// Колбэки (могут быть nil). Вызываются вне mu.
	OnLog   func(line string)
	OnState func(state State, reason string)
}

// NewManager создаёт менеджер ядра с разрешёнными путями.
func NewManager(paths *Paths) *Manager {
	return &Manager{paths: paths, state: StateStopped}
}

// SetBinaryPath задаёт путь к альтернативному sing-box.exe (пусто = встроенный).
func (m *Manager) SetBinaryPath(path string) {
	m.mu.Lock()
	m.binaryPath = path
	m.mu.Unlock()
}

// BinaryPath возвращает эффективный путь к бинарнику ядра.
func (m *Manager) BinaryPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binaryPath != "" {
		return m.binaryPath
	}
	return m.paths.SingBox
}

// State возвращает текущее состояние.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Logs возвращает копию накопленного лога.
func (m *Manager) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.logs))
	copy(out, m.logs)
	return out
}

// Start пишет config на диск и запускает sing-box. Возвращает ошибку, если
// ядро уже запущено, отсутствует бинарник или процесс не удалось стартовать.
func (m *Manager) Start(configJSON []byte) error {
	m.mu.Lock()
	if m.state == StateRunning || m.state == StateStarting {
		m.mu.Unlock()
		return fmt.Errorf("ядро уже запущено")
	}
	m.state = StateStarting
	m.mu.Unlock()
	m.emitState(StateStarting, "")

	binary := m.BinaryPath()
	if !fileExists(binary) {
		m.setState(StateError, "не найден sing-box.exe")
		return fmt.Errorf("не найден sing-box.exe по пути %s", binary)
	}

	if err := os.MkdirAll(m.paths.DataDir, 0o755); err != nil {
		m.setState(StateError, "не удалось создать каталог данных")
		return err
	}
	if err := os.WriteFile(m.paths.ConfigPath, configJSON, 0o644); err != nil {
		m.setState(StateError, "не удалось записать config.json")
		return err
	}

	// Открываем box.log заново на каждый запуск (перезаписываем прошлый прогон).
	// Ошибку не считаем фатальной — лог в UI и кольцевом буфере всё равно есть.
	if f, err := os.Create(filepath.Join(m.paths.DataDir, "box.log")); err == nil {
		m.mu.Lock()
		m.logFile = f
		m.mu.Unlock()
	}

	// sing-box резолвит относительные пути (geo-базы, cache) от рабочего каталога.
	cmd := exec.Command(binary, "run", "-c", m.paths.ConfigPath, "-D", m.paths.DataDir)
	cmd.Dir = m.paths.DataDir
	applySysProcAttr(cmd) // Windows: скрыть консольное окно

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.setState(StateError, err.Error())
		return err
	}
	cmd.Stderr = cmd.Stdout // sing-box пишет логи в stderr; сведём в один поток

	if err := cmd.Start(); err != nil {
		m.setState(StateError, "не удалось запустить процесс")
		return fmt.Errorf("запуск sing-box: %w", err)
	}
	superviseChild(cmd.Process.Pid) // Windows: привязать к job object (kill-on-close)

	m.mu.Lock()
	m.cmd = cmd
	m.state = StateRunning
	m.mu.Unlock()
	m.emitState(StateRunning, "")

	go m.readLoop(bufio.NewReader(stdout))
	go m.waitLoop(cmd)

	return nil
}

// Stop корректно останавливает ядро вместе с потомками.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	if cmd == nil || cmd.Process == nil {
		m.mu.Unlock()
		return nil
	}
	pid := cmd.Process.Pid
	m.mu.Unlock()

	killProcessTree(pid) // Windows: taskkill /T /F с фолбэком

	// Дадим процессу немного времени завершиться; waitLoop переведёт состояние.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.State() != StateRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// readLoop построчно читает вывод ядра в кольцевой буфер и в колбэк.
func (m *Manager) readLoop(r *bufio.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := stripANSI(scanner.Text())
		if line == "" {
			continue
		}
		m.appendLog(line)
	}
}

// waitLoop дожидается завершения процесса и обновляет состояние.
func (m *Manager) waitLoop(cmd *exec.Cmd) {
	err := cmd.Wait()

	m.mu.Lock()
	// Если это всё ещё «наш» процесс — сбрасываем.
	if m.cmd == cmd {
		m.cmd = nil
	}
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	m.mu.Unlock()

	reason := ""
	next := StateStopped
	if err != nil {
		reason = err.Error()
		next = StateStopped // остановка по taskkill тоже приходит как ошибка — это норма
	}
	m.setState(next, reason)
}

func (m *Manager) appendLog(line string) {
	m.mu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
	if m.logFile != nil {
		_, _ = m.logFile.WriteString(line + "\n")
	}
	m.mu.Unlock()

	if m.OnLog != nil {
		m.OnLog(line)
	}
}

func (m *Manager) setState(s State, reason string) {
	m.mu.Lock()
	m.state = s
	m.mu.Unlock()
	m.emitState(s, reason)
}

func (m *Manager) emitState(s State, reason string) {
	if m.OnState != nil {
		m.OnState(s, reason)
	}
}

// stripANSI убирает управляющие ANSI-последовательности из строки лога.
func stripANSI(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b { // ESC
			// пропускаем до буквы (конца CSI-последовательности)
			j := i + 1
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
