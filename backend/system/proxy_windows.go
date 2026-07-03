// Package system содержит Windows-специфичную интеграцию: системный прокси,
// повышение прав (UAC), автозапуск.
package system

import (
	"fmt"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const proxyRegPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// не заворачиваем локальные адреса через прокси
const proxyBypass = "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*;<local>"

// SystemProxy управляет системным HTTP-прокси Windows с бэкапом прежних настроек.
type SystemProxy struct {
	mu       sync.Mutex
	active   bool
	backup   proxyBackup
	hasBack  bool
}

type proxyBackup struct {
	enable   uint32
	server   string
	override string
}

// NewSystemProxy создаёт контроллер системного прокси.
func NewSystemProxy() *SystemProxy { return &SystemProxy{} }

// Set включает системный прокси на адрес вида "127.0.0.1:2080",
// предварительно сохранив текущие настройки пользователя.
func (s *SystemProxy) Set(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, proxyRegPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("открытие ключа реестра: %w", err)
	}
	defer k.Close()

	if !s.hasBack {
		s.backup = readBackup(k)
		s.hasBack = true
	}

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", proxyBypass); err != nil {
		return err
	}

	s.active = true
	notifyWinInet()
	return nil
}

// Clear возвращает системный прокси к состоянию до включения.
// Безопасно вызывать многократно и когда прокси не был установлен.
func (s *SystemProxy) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return nil
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, proxyRegPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("открытие ключа реестра: %w", err)
	}
	defer k.Close()

	if s.hasBack {
		_ = k.SetDWordValue("ProxyEnable", s.backup.enable)
		if s.backup.server != "" {
			_ = k.SetStringValue("ProxyServer", s.backup.server)
		}
		if s.backup.override != "" {
			_ = k.SetStringValue("ProxyOverride", s.backup.override)
		}
	} else {
		_ = k.SetDWordValue("ProxyEnable", 0)
	}

	s.active = false
	notifyWinInet()
	return nil
}

// Active сообщает, включён ли сейчас системный прокси нами.
func (s *SystemProxy) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func readBackup(k registry.Key) proxyBackup {
	var b proxyBackup
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		b.enable = uint32(v)
	}
	if v, _, err := k.GetStringValue("ProxyServer"); err == nil {
		b.server = v
	}
	if v, _, err := k.GetStringValue("ProxyOverride"); err == nil {
		b.override = v
	}
	return b
}

// notifyWinInet сообщает системе, что настройки прокси изменились,
// чтобы браузеры и WinINet-приложения подхватили их без перезапуска.
func notifyWinInet() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	const (
		INTERNET_OPTION_SETTINGS_CHANGED = 39
		INTERNET_OPTION_REFRESH          = 37
	)
	setOption.Call(0, INTERNET_OPTION_SETTINGS_CHANGED, 0, 0)
	setOption.Call(0, INTERNET_OPTION_REFRESH, 0, 0)
}
