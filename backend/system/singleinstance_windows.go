package system

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Защита от второй копии приложения.
//
// Встроенный в Wails options.SingleInstanceLock здесь не годится: обе его дыры
// открываются ровно после UAC-перезапуска ради TUN, то есть в самом ходовом
// сценарии приложения.
//
//   - Процесс, поднятый с повышением прав, лок не регистрирует вовсе — иначе он
//     опознал бы ещё живого предка как «уже запущено» и завершился, не подняв TUN.
//     С этого момента защиты нет ни у кого.
//   - Именованный мьютекс, созданный elevated-процессом, наследует его уровень
//     целостности, поэтому обычный запуск получает на нём ACCESS_DENIED. Wails
//     считает это неизвестной ошибкой и штатно поднимает вторую копию: вторая
//     иконка в трее, второе ядро и драка за порты 2080/9090.
//
// Поэтому механизм свой. Первая копия создаёт именованное событие и ждёт на нём;
// вторая при создании получает ERROR_ALREADY_EXISTS — то есть дескриптор того же
// объекта, — взводит его и уходит. Одной операцией вместо «проверить, потом
// создать»: двум запущенным одновременно копиям негде разойтись.
const instanceEventName = "MitM-single-instance-1f6a2b"

// instanceEventSDDL — права на событие: полный доступ всем (D:(A;;GA;;;WD)) плюс
// низкая метка целостности (S:(ML;;NW;;;LW)).
//
// Метка обязательна: без неё событие, созданное elevated-копией, недоступно
// обычному запуску — это и есть вторая из описанных выше дыр. Щедрость DACL
// ничего не стоит: единственное, что даёт это событие, — просьба показать окно.
const instanceEventSDDL = "D:(A;;GA;;;WD)S:(ML;;NW;;;LW)"

// ClaimSingleInstance объявляет процесс единственной копией приложения.
//
// Возвращает false, если копия уже работает: ей отправлена просьба показать окно
// (у неё сработает onActivate), а вызывающему остаётся тихо завершиться. При
// успехе остаётся жить горутина, вызывающая onActivate на каждую такую просьбу.
//
// Любая непонятная ошибка трактуется как «мы первые»: не запуститься вовсе куда
// хуже, чем показать лишнюю копию.
func ClaimSingleInstance(onActivate func()) bool {
	return claimInstance(instanceEventName, onActivate)
}

// claimInstance — то же самое с явным именем события: тесты берут своё, чтобы не
// драться с работающим приложением.
func claimInstance(eventName string, onActivate func()) bool {
	name, err := syscall.UTF16PtrFromString(eventName)
	if err != nil {
		return true
	}

	// Без метки целостности защита остаётся, но только в пределах одного уровня
	// прав — это лучше, чем не создать событие совсем.
	sa, _ := instanceEventAttrs()

	// Событие со сбросом вручную нам не нужно: каждая просьба должна разбудить
	// ожидающую горутину ровно один раз.
	ev, err := windows.CreateEvent(sa, 0, 0, name)
	switch {
	case err == nil:
		go watchInstanceEvent(ev, onActivate)
		return true

	case errors.Is(err, windows.ERROR_ALREADY_EXISTS):
		// Просим работающую копию показаться и заодно уступаем ей право выйти на
		// передний план: активный процесс сейчас мы, и без этой уступки Windows
		// не даст чужому окну перехватить фокус — оно всплывёт под остальными.
		allowSetForegroundWindow()
		_ = windows.SetEvent(ev)
		_ = windows.CloseHandle(ev)
		return false

	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		// Событие создано копией с большими правами и без низкой метки (сборка
		// до 2.0.1). Достучаться до неё не выйдет, но она точно есть.
		return false

	default:
		return true
	}
}

// watchInstanceEvent ждёт просьб от новых запусков до самого конца процесса.
func watchInstanceEvent(ev windows.Handle, onActivate func()) {
	for {
		st, err := windows.WaitForSingleObject(ev, windows.INFINITE)
		if err != nil || st != windows.WAIT_OBJECT_0 {
			return
		}
		if onActivate != nil {
			onActivate()
		}
	}
}

// instanceEventAttrs собирает SECURITY_ATTRIBUTES из instanceEventSDDL.
func instanceEventAttrs() (*windows.SecurityAttributes, error) {
	sd, err := windows.SecurityDescriptorFromString(instanceEventSDDL)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa, nil
}

// allowSetForegroundWindow разрешает любому процессу вывести своё окно на передний
// план от нашего имени. PID работающей копии нам неизвестен, поэтому ASFW_ANY;
// уступка живёт до следующей смены активного окна и ничем не грозит.
func allowSetForegroundWindow() {
	const asfwAny = ^uintptr(0) // (DWORD)-1
	user32 := syscall.NewLazyDLL("user32.dll")
	_, _, _ = user32.NewProc("AllowSetForegroundWindow").Call(asfwAny)
}
