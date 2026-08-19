package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsAdmin сообщает, запущен ли процесс с правами администратора (elevated).
func IsAdmin() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

// ErrElevationCancelled возвращается, когда пользователь отклонил запрос UAC.
var ErrElevationCancelled = fmt.Errorf("повышение прав отменено пользователем")

// RelaunchElevated перезапускает текущий исполняемый файл с правами администратора
// (появится диалог UAC), передавая ему extraArgs. Текущий процесс НЕ завершается —
// это решает вызывающая сторона после успешного запуска.
func RelaunchElevated(extraArgs ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	args := strings.Join(quoteArgs(extraArgs), " ")
	return shellExecuteRunAs(exe, args, cwd)
}

// WaitForProcessExit ждёт завершения процесса с указанным PID, но не дольше timeout.
// Возвращает true, если процесса уже нет. Нужно при UAC-перезапуске: ShellExecuteW
// возвращает управление сразу после старта нового процесса, поэтому предок в этот
// момент ещё жив и держит ресурсы, которые нельзя делить (см. WaitForWebviewRelease).
func WaitForProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true // процесса уже нет либо он нам недоступен — ждать нечего
	}
	defer windows.CloseHandle(h)

	ev, err := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

// WaitForWebviewRelease ждёт, пока WebView2 предыдущего процесса отпустит свою
// user data folder, но не дольше timeout. Возвращает true, если папка свободна.
//
// Зачем: WebView2 держит `lockfile` в профиле эксклюзивно, а обычный и elevated
// процессы делят одну папку и не могут работать с ней одновременно — у второго
// движок не поднимется и окно останется пустым (только BackgroundColour). Процессы
// браузера переживают своего хозяина на доли секунды, поэтому ожидания одного лишь
// PID мало. Всё best-effort: не нашли файл — считаем, что путь свободен.
func WaitForWebviewRelease(timeout time.Duration) bool {
	lock := webviewLockPath()
	if lock == "" {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		if webviewLockFree(lock) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// webviewLockPath — путь к lockfile профиля WebView2. По умолчанию движок кладёт
// профиль в %APPDATA%\<имя exe с расширением>\EBWebView.
func webviewLockPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(appData, filepath.Base(exe), "EBWebView", "lockfile")
}

// webviewLockFree проверяет, отпущен ли lockfile: живой WebView2 держит его без
// права совместного доступа, поэтому открытие падает с ERROR_SHARING_VIOLATION.
func webviewLockFree(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return os.IsNotExist(err) // файла нет — профиль точно никем не занят
	}
	_ = f.Close()
	return true
}

// shellExecuteRunAs вызывает ShellExecuteW с глаголом "runas" (запуск с повышением).
func shellExecuteRunAs(exe, params, dir string) error {
	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	var paramPtr *uint16
	if params != "" {
		paramPtr, _ = syscall.UTF16PtrFromString(params)
	}
	var dirPtr *uint16
	if dir != "" {
		dirPtr, _ = syscall.UTF16PtrFromString(dir)
	}
	const SW_SHOWNORMAL = 1

	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(paramPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(SW_SHOWNORMAL),
	)
	// ShellExecuteW возвращает значение > 32 при успехе.
	if ret <= 32 {
		const SE_ERR_ACCESSDENIED = 5 // пользователь отклонил UAC (ERROR_CANCELLED)
		if ret == SE_ERR_ACCESSDENIED || ret == 1223 {
			return ErrElevationCancelled
		}
		return fmt.Errorf("ShellExecuteW не удался, код %d", ret)
	}
	return nil
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			out[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			out[i] = a
		}
	}
	return out
}
