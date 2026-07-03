package system

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// Автозапуск реализуем через HKCU\...\Run — не требует прав администратора и
// работает для портативного exe (значение — путь к текущему файлу).
const (
	autostartKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartName = "Proxy"
)

// SetAutostart включает или выключает автозапуск приложения вместе с Windows.
// При включении приложение стартует в свёрнутом (в трей) виде — флаг --minimized.
func SetAutostart(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if !enable {
		if err := k.DeleteValue(autostartName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(autostartName, fmt.Sprintf("\"%s\" %s", exe, MinimizedFlag))
}

// AutostartEnabled сообщает, прописан ли автозапуск в реестре.
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autostartName)
	return err == nil
}

// MinimizedFlag передаётся при автозапуске — приложение стартует свёрнутым в трей.
const MinimizedFlag = "--minimized"
