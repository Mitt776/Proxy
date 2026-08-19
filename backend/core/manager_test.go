// Юнит-тесты жизненного цикла ядра. Настоящий sing-box тут не нужен: роль ядра
// играет крошечный процесс-заглушка, собранный на лету, — важно поведение
// Manager (переходы состояний, ожидание смерти процесса, тишина при рестарте),
// а не то, что именно запущено. Тесты на живом ядре — под тегом coretest.
package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeCore собирает процесс-заглушку, который просто живёт, пока его не убьют,
// и игнорирует аргументы (Manager всегда зовёт "run -c … -D …").
func fakeCore(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go не найден в PATH — нечем собрать заглушку ядра")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	// Спим, а не блокируемся на пустом select: пустой select рантайм Go считает
	// дедлоком и завершает процесс сам — заглушка умерла бы, не дождавшись kill.
	code := "package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(10 * time.Minute) }\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fakecore.exe")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("не собралась заглушка ядра: %v\n%s", err, out)
	}
	return bin
}

// newTestManager даёт менеджер, указывающий на заглушку вместо sing-box.
func newTestManager(t *testing.T, binary string) *Manager {
	t.Helper()
	data := t.TempDir()
	m := NewManager(&Paths{
		DataDir:    data,
		AssetsDir:  data,
		ConfigPath: filepath.Join(data, "config.json"),
		SingBox:    binary,
	})
	t.Cleanup(func() { _ = m.Stop() })
	return m
}

// stateRecorder копит состояния, которые Manager отдал наверх.
type stateRecorder struct {
	mu     sync.Mutex
	states []State
}

func (r *stateRecorder) add(s State) {
	r.mu.Lock()
	r.states = append(r.states, s)
	r.mu.Unlock()
}

func (r *stateRecorder) snapshot() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]State, len(r.states))
	copy(out, r.states)
	return out
}

func TestStopOnIdleManager(t *testing.T) {
	m := newTestManager(t, "нет-такого-файла.exe")
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop на незапущенном ядре вернул ошибку: %v", err)
	}
	if m.State() != StateStopped {
		t.Fatalf("состояние = %q, want stopped", m.State())
	}
}

func TestStartMissingBinary(t *testing.T) {
	m := newTestManager(t, filepath.Join(t.TempDir(), "нет-ядра.exe"))
	rec := &stateRecorder{}
	m.OnState = func(s State, _ string) { rec.add(s) }

	if err := m.Start([]byte("{}")); err == nil {
		t.Fatal("ожидалась ошибка: бинарника ядра нет")
	}
	if m.State() != StateError {
		t.Fatalf("состояние = %q, want error", m.State())
	}
	// Состояние не должно залипнуть в starting — иначе повторный Start
	// откажется работать («ядро уже запущено») до перезапуска приложения.
	if err := m.Start([]byte("{}")); err == nil {
		t.Fatal("ожидалась ошибка и на повторном Start")
	}
}

// TestStopWaitsForProcessDeath: Stop обязан возвращаться только после реальной
// смерти процесса. Прежняя версия ждала 3 с поллингом и молча отдавала nil —
// вызывающий поднимал новое ядро поверх живого, и оно падало на занятых портах.
func TestStopWaitsForProcessDeath(t *testing.T) {
	m := newTestManager(t, fakeCore(t))
	if err := m.Start([]byte("{}")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if m.State() != StateRunning {
		t.Fatalf("состояние = %q, want running", m.State())
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s := m.State(); s != StateStopped {
		t.Fatalf("сразу после Stop состояние = %q, want stopped", s)
	}
	// Порты освободились — новое ядро поднимается без ожиданий.
	if err := m.Start([]byte("{}")); err != nil {
		t.Fatalf("повторный Start после Stop: %v", err)
	}
}

// TestRestartHidesStoppedState: пока идёт перезапуск, наверх не должно уйти
// «остановлено». В app.go это событие снимает системный прокси — правка правила
// оставила бы пользователя без интернета на ровном месте.
func TestRestartHidesStoppedState(t *testing.T) {
	m := newTestManager(t, fakeCore(t))
	if err := m.Start([]byte("{}")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Заглушка обязана быть живой, иначе Restart свёлся бы к обычному Start и
	// тест проверял бы не то.
	time.Sleep(200 * time.Millisecond)
	if s := m.State(); s != StateRunning {
		t.Fatalf("заглушка ядра не живёт: состояние = %q", s)
	}

	rec := &stateRecorder{}
	m.OnState = func(s State, _ string) { rec.add(s) }

	if err := m.Restart([]byte("{}")); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if m.State() != StateRunning {
		t.Fatalf("после Restart состояние = %q, want running", m.State())
	}

	// Дадим шанс запоздалому событию от старого процесса.
	time.Sleep(300 * time.Millisecond)
	for _, s := range rec.snapshot() {
		if s == StateStopped {
			t.Fatalf("во время Restart наверх ушло %q: %v", s, rec.snapshot())
		}
	}
}
