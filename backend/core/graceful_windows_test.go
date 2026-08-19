// Проверка штатной остановки ядра: Ctrl+Break должен доставаться процессу
// ядра, а не нам. Обе половины важны — ядро без сигнала не успевает снять
// TUN-маршруты (у пользователя пропадает интернет), а лишний сигнал себе
// закрывает всё приложение при нажатии «Отключить».
package core

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// gracefulHelper собирает заглушку, ведущую себя как sing-box: живёт, пока не
// получит сигнал (в него рантайм Go превращает Ctrl+Break), и только тогда
// «прибирается» — печатает cleaned и выходит с нулевым кодом.
func gracefulHelper(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go не найден в PATH — нечем собрать заглушку ядра")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	w := bufio.NewWriter(os.Stdout)
	fmt.Fprintln(w, "ready")
	w.Flush()
	select {
	case <-ch:
		fmt.Fprintln(w, "cleaned") // здесь настоящее ядро снимает TUN-маршруты
		w.Flush()
		os.Exit(0)
	case <-time.After(20 * time.Second):
		os.Exit(2) // сигнал не пришёл
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "gracefulcore.exe")
	build := exec.Command("go", "build", "-o", bin, src)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("не собралась заглушка ядра: %v\n%s", err, out)
	}
	return bin
}

// TestCtrlBreakStopsOnlyTargetGroup проверяет обе стороны фикса: сигнал
// доходит до ядра и оно завершается штатно, а процесс-отправитель остаётся жив.
//
// Тест повторяет механику requestGracefulStop, кроме AttachConsole: у `go test`
// уже есть собственная консоль, поэтому прицепиться к чужой он не может (это
// нужно только GUI-процессу без консоли). Заглушка наследует консоль теста и
// получает собственную группу — ровно так, как это делает applySysProcAttr.
func TestCtrlBreakStopsOnlyTargetGroup(t *testing.T) {
	bin := gracefulHelper(t)

	cmd := exec.Command(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("запуск заглушки: %v", err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			killProcessTree(cmd.Process.Pid)
		}
	}()

	lines := bufio.NewScanner(stdout)
	if !lines.Scan() || lines.Text() != "ready" {
		t.Fatalf("заглушка не сообщила о готовности (получено %q)", lines.Text())
	}

	shieldFromConsoleCtrl()
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid)); err != nil {
		t.Skipf("консоль недоступна — проверять нечего: %v", err)
	}

	// Отправитель жив: сигнал ушёл в группу заглушки и до нас не дошёл. Именно
	// это и было сломано — событие всей консоли (processGroupID=0) закрывало
	// приложение при нажатии «Отключить».
	//
	// Ремень безопасности shieldFromConsoleCtrl этой проверкой НЕ покрыт: он
	// нужен на случай, если событие всё же долетит до нас, а безопасно
	// устроить такое в тесте нельзя — пришлось бы слать Ctrl+Break всей
	// консоли, вместе с терминалом, из которого запущены тесты.
	t.Log("✅ процесс-отправитель пережил Ctrl+Break")

	if !lines.Scan() || lines.Text() != "cleaned" {
		t.Fatalf("ядро не успело прибраться перед смертью (получено %q)", lines.Text())
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("заглушка завершилась не штатно: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("заглушка не завершилась после Ctrl+Break")
	}
	t.Log("✅ ядро получило сигнал и завершилось штатно")
}
