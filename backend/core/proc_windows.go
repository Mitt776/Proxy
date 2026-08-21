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

const (
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
)

// applySysProcAttr прячет консольное окно sing-box, чтобы оно не мелькало.
//
// CREATE_NEW_PROCESS_GROUP обязателен для штатной остановки: только имея
// собственную группу, ядро может получить Ctrl+Break адресно, не задевая наш
// GUI-процесс (см. requestGracefulStop).
func applySysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | createNewProcessGroup,
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

// killProcessTree жёстко завершает процесс sing-box вместе со всеми потомками
// через taskkill /T /F. Быстро и надёжно освобождает порты, но НЕ даёт ядру
// шанса на собственную очистку — см. requestGracefulStop, который нужно звать
// первым при живом TUN.
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

var (
	kernel32DLL               = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole         = kernel32DLL.NewProc("AttachConsole")
	procFreeConsole           = kernel32DLL.NewProc("FreeConsole")
	procSetConsoleCtrlHandler = kernel32DLL.NewProc("SetConsoleCtrlHandler")
)

var ctrlHandlerOnce sync.Once

// shieldFromConsoleCtrl ставит собственный обработчик Ctrl-событий, который
// сообщает системе «обработано» вместо завершения процесса.
//
// Без него приложение закрывается вместе с ядром: пока мы прицеплены к его
// консоли, Ctrl-событие достаётся и нам, а обработчика по умолчанию у GUI нет —
// процесс просто умирает. Штатный «глушитель» SetConsoleCtrlHandler(NULL, TRUE)
// тут не помогает: он подавляет только Ctrl+C, но не Ctrl+Break.
//
// CTRL_CLOSE/LOGOFF/SHUTDOWN намеренно пропускаем дальше (FALSE) — мешать
// выключению системы нельзя.
func shieldFromConsoleCtrl() {
	ctrlHandlerOnce.Do(func() {
		cb := syscall.NewCallback(func(ctrlType uint32) uintptr {
			switch ctrlType {
			case windows.CTRL_C_EVENT, windows.CTRL_BREAK_EVENT:
				return 1 // TRUE: событие обработано, не завершаться
			}
			return 0 // остальное — системе виднее
		})
		procSetConsoleCtrlHandler.Call(cb, 1)
	})
}

// requestGracefulStop просит sing-box завершиться самостоятельно (аналог
// Ctrl+Break в консоли). Это единственный способ дать ядру время снять
// TUN-маршруты (auto_route/strict_route) и удалить wintun-адаптер в своём
// defer — killProcessTree такого шанса не оставляет: адаптер и подменённые
// маршруты остаются в системе, и у пользователя пропадает интернет даже
// после закрытия приложения (симптом — браузер офлайн, хотя ядро уже мертво).
//
// Wails-процесс — GUI (windows-subsystem) и своей консоли не имеет, поэтому
// послать событие напрямую нельзя: сначала цепляемся к консоли ядра через
// AttachConsole (её sing-box получил автоматически, как консольный процесс без
// унаследованной консоли).
//
// Сигнал адресуется **группе процессов ядра**, а не всей консоли
// (processGroupID=0): ядро запущено с CREATE_NEW_PROCESS_GROUP, наш процесс в
// эту группу не входит и события не получает. Плюс shieldFromConsoleCtrl как
// вторая линия обороны — иначе «Отключить» закрывало бы всё приложение.
//
// Возвращает false, если прицепиться не удалось (ядро уже мертво или что-то
// пошло не так) — тогда ждать нечего, сразу переходим к killProcessTree.
func requestGracefulStop(pid int) bool {
	shieldFromConsoleCtrl()

	if r, _, _ := procAttachConsole.Call(uintptr(pid)); r == 0 {
		return false
	}
	defer procFreeConsole.Call()

	// Группа процессов ядра совпадает по идентификатору с его pid: группу
	// создал сам CreateProcess по флагу CREATE_NEW_PROCESS_GROUP.
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid)) == nil
}
