package system

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LOCALE_NAME_MAX_LENGTH из winnls.h — потолок длины имени локали вида "ru-RU".
const localeNameMaxLength = 85

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultLocaleNam = kernel32.NewProc("GetUserDefaultLocaleName")
)

// DefaultLang возвращает язык интерфейса по умолчанию для текущего пользователя:
// "ru" для русскоязычной системы, "en" для всех остальных. Нужен ровно один раз —
// при первом запуске, пока пользователь не выбрал язык сам (settings.Lang пуст).
func DefaultLang() string {
	buf := make([]uint16, localeNameMaxLength)
	r, _, _ := procGetUserDefaultLocaleNam.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r == 0 {
		return "en" // API не ответил — английский как безопасный дефолт
	}
	name := syscall.UTF16ToString(buf[:r])
	if strings.HasPrefix(strings.ToLower(name), "ru") {
		return "ru"
	}
	return "en"
}
