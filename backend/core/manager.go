// Package core управляет жизненным циклом внешнего процесса sing-box.
package core

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// stopTimeout — сколько ждём фактической смерти процесса после killProcessTree.
const stopTimeout = 5 * time.Second

// gracefulTimeout — сколько ждём ядро после requestGracefulStop, прежде чем
// эскалировать до жёсткого killProcessTree. sing-box снимает TUN-маршруты и
// удаляет wintun-адаптер в defer при штатном завершении; таймаут — на случай
// зависшего ядра, а не обычный путь (обычно укладывается в доли секунды).
const gracefulTimeout = 3 * time.Second

// Manager запускает и останавливает sing-box.exe и собирает его вывод.
// Он не зависит от Wails: наверх отдаёт события через колбэки OnLog/OnState,
// которые app.go проксирует в runtime-события фронтенда.
type Manager struct {
	paths *Paths

	mu         sync.Mutex
	cmd        *exec.Cmd
	done       chan struct{} // закрывается, когда процесс cmd действительно умер
	state      State
	logs       []string
	logFile    *os.File // дубликат вывода ядра на диск (box.log) для диагностики
	binaryPath string   // альтернативный sing-box.exe (пусто = paths.SingBox)
	// restarting — идёт перезапуск с новым конфигом. Пока флаг взведён, смерть
	// процесса не порождает событие «остановлено»: иначе слушатель наверху решит,
	// что соединение оборвалось, и снимет системный прокси у живого туннеля.
	restarting bool

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
		m.closeLogFile() // иначе дескриптор box.log течёт до выхода из приложения
		m.setState(StateError, err.Error())
		return err
	}
	cmd.Stderr = cmd.Stdout // sing-box пишет логи в stderr; сведём в один поток

	if err := cmd.Start(); err != nil {
		m.closeLogFile()
		m.setState(StateError, "не удалось запустить процесс")
		return fmt.Errorf("запуск sing-box: %w", err)
	}
	superviseChild(cmd.Process.Pid) // Windows: привязать к job object (kill-on-close)

	done := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.done = done
	m.state = StateRunning
	m.mu.Unlock()
	m.emitState(StateRunning, "")

	go m.readLoop(bufio.NewReader(stdout))
	go m.waitLoop(cmd, done)

	return nil
}

// Check проверяет конфиг настоящим `sing-box check` и возвращает вывод ядра
// как ошибку. Дешевле и безопаснее, чем узнать о поломке по упавшему процессу:
// вызывается перед стартом и перезапуском.
func (m *Manager) Check(configJSON []byte) error {
	binary := m.BinaryPath()
	if !fileExists(binary) {
		return fmt.Errorf("не найден sing-box.exe по пути %s", binary)
	}
	if err := os.MkdirAll(m.paths.DataDir, 0o755); err != nil {
		return err
	}
	// Проверяем в рабочем каталоге ядра: относительные пути (cache.db, гео-базы)
	// должны резолвиться так же, как при реальном запуске.
	f, err := os.CreateTemp(m.paths.DataDir, "check-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(configJSON)
	_ = f.Close()
	defer os.Remove(tmp)
	if werr != nil {
		return werr
	}

	cmd := exec.Command(binary, "check", "-c", tmp, "-D", m.paths.DataDir)
	cmd.Dir = m.paths.DataDir
	applySysProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(stripANSI(string(out)))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("конфигурация отвергнута ядром: %s", msg)
	}
	return nil
}

// Restart поднимает ядро с новым конфигом, не показывая наверх промежуточное
// состояние «остановлено» (см. поле restarting). Если ядро не работало —
// эквивалентен Start.
func (m *Manager) Restart(configJSON []byte) error {
	if m.State() != StateRunning {
		return m.Start(configJSON)
	}

	m.mu.Lock()
	m.restarting = true
	m.mu.Unlock()

	// Stop возвращается только после фактической смерти процесса (waitLoop успел
	// отработать и закрыть done), поэтому флаг снимаем строго после него: сбрось
	// мы его раньше — waitLoop сообщил бы наверх «остановлено», и App снял бы
	// системный прокси у живого соединения.
	err := m.Stop()

	m.mu.Lock()
	m.restarting = false
	m.state = StateStopped // Start откажется стартовать поверх running/starting
	m.mu.Unlock()

	if err != nil {
		// Старое ядро не умерло: новое не займёт порты, и молчать об этом нельзя.
		m.setState(StateError, err.Error())
		return err
	}
	return m.Start(configJSON)
}

// Stop останавливает ядро вместе с потомками и дожидается смерти процесса.
// Ошибка означает, что процесс пережил таймаут: соврать здесь нельзя — поверх
// живого ядра новое не поднимется, у него заняты порты (mixed 2080, API 9090).
//
// Сначала просим ядро завершиться штатно (requestGracefulStop) и ждём
// gracefulTimeout: только так при активном TUN снимаются auto_route-маршруты
// и удаляется wintun-адаптер (see proc_windows.go). Жёсткий killProcessTree —
// эскалация для зависшего ядра, а не путь по умолчанию: он не даёт ядру
// шанса на очистку, и в системе остаются «осиротевшие» TUN-маршруты —
// у пользователя пропадает интернет даже после закрытия приложения.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	done := m.done
	if cmd == nil || cmd.Process == nil {
		m.mu.Unlock()
		return nil
	}
	pid := cmd.Process.Pid
	m.mu.Unlock()

	if done == nil { // процесс есть, а waitLoop не запущен — ждать нечего
		killProcessTree(pid)
		return nil
	}

	if requestGracefulStop(pid) {
		select {
		case <-done:
			return nil
		case <-time.After(gracefulTimeout):
			// не отреагировало — переходим к жёсткой остановке ниже
		}
	}

	killProcessTree(pid) // Windows: taskkill /T /F с фолбэком

	select {
	case <-done: // waitLoop дождался Wait() и обновил состояние
		return nil
	case <-time.After(stopTimeout):
		return fmt.Errorf("ядро (pid %d) не завершилось за %s", pid, stopTimeout)
	}
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
func (m *Manager) waitLoop(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()

	m.mu.Lock()
	// Если это всё ещё «наш» процесс — сбрасываем.
	if m.cmd == cmd {
		m.cmd = nil
		m.done = nil
	}
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	restarting := m.restarting
	if restarting {
		m.state = StateStopped
	}
	m.mu.Unlock()

	// Только теперь процесса точно нет — отпускаем ожидающий Stop.
	close(done)

	// Перезапуск: наверх ничего не сообщаем — состояние обновит следующий Start.
	if restarting {
		return
	}

	reason := ""
	next := StateStopped
	if err != nil {
		reason = err.Error()
		next = StateStopped // остановка по taskkill тоже приходит как ошибка — это норма
	}
	m.setState(next, reason)
}

// closeLogFile закрывает box.log, открытый под текущий (не состоявшийся) запуск.
func (m *Manager) closeLogFile() {
	m.mu.Lock()
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	m.mu.Unlock()
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

// ruleSetMatchLine выхватывает индекс правила из строки вида
// "match rules.[2]: domain_suffix=example.com".
var ruleSetMatchLine = regexp.MustCompile(`rules\.\[(\d+)\]`)

// RuleSetMatch спрашивает у ядра, попадает ли домен (или IP) в набор правил, и
// возвращает индексы совпавших правил внутри набора. Формат — "binary" для .srs
// или "source" для JSON.
//
// Ядро зовём вместо собственной реализации матчинга намеренно: у domain_suffix и
// domain_regex в sing-box своя семантика, и расходиться с ней проверка не должна.
func (m *Manager) RuleSetMatch(path, format, value string) ([]int, error) {
	binary := m.BinaryPath()
	if !fileExists(binary) {
		return nil, fmt.Errorf("не найден sing-box.exe по пути %s", binary)
	}
	if format == "" {
		format = "binary"
	}
	cmd := exec.Command(binary, "rule-set", "match", path, value, "-f", format)
	cmd.Dir = m.paths.DataDir
	applySysProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(stripANSI(string(out)))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return nil, fmt.Errorf("проверка набора правил: %s", text)
	}
	var idx []int
	for _, line := range strings.Split(text, "\n") {
		mm := ruleSetMatchLine.FindStringSubmatch(line)
		if mm == nil {
			continue
		}
		n, convErr := strconv.Atoi(mm[1])
		if convErr != nil {
			continue
		}
		idx = append(idx, n)
	}
	return idx, nil
}
