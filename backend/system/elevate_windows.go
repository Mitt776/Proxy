package system

import (
	"fmt"
	"os"
	"strings"
	"syscall"
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
