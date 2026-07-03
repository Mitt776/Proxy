package core

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// applySysProcAttr прячет консольное окно sing-box, чтобы оно не мелькало.
func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
	jobErr    error
)

// superviseChild привязывает процесс ядра к Job Object с флагом
// KILL_ON_JOB_CLOSE. Тогда при завершении GUI (в т.ч. аварийном) ОС сама
// закрывает описатель job'а и убивает ядро — TUN-адаптер не «залипает».
// Ошибки не критичны: остаётся штатный killProcessTree при выходе.
func superviseChild(pid int) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = err
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			jobErr = err
			_ = windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
	if jobErr != nil || jobHandle == 0 {
		return
	}

	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(ph)
	_ = windows.AssignProcessToJobObject(jobHandle, ph)
}

// killProcessTree завершает процесс sing-box вместе со всеми потомками.
// Используем taskkill /T /F — это надёжно снимает дерево и освобождает TUN-адаптер;
// при неудаче падаем на прямой Kill найденного процесса.
func killProcessTree(pid int) {
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := kill.Run(); err == nil {
		return
	}
	if p, err := os.FindProcess(pid); err == nil && p != nil {
		_ = p.Kill()
	}
}
