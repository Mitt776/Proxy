// Package system содержит Windows-специфичную интеграцию: системный прокси,
// повышение прав (UAC), автозапуск.
package system

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const proxyRegPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// не заворачиваем локальные адреса через прокси
const proxyBypass = "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.2*;172.30.*;172.31.*;192.168.*;<local>"

// SystemProxy управляет системным HTTP-прокси Windows с бэкапом прежних настроек.
type SystemProxy struct {
	mu      sync.Mutex
	active  bool
	addr    string // адрес, который выставили мы (для распознавания «своего» прокси)
	backup  proxyBackup
	hasBack bool
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
		b := readBackup(k)
		// В реестре уже стоит наш адрес — значит прошлый запуск умер, не убравшись.
		// Такой «бэкап» восстанавливать нельзя: Clear вернул бы прокси на мёртвый
		// порт и оставил пользователя без интернета.
		if sameProxyAddr(b.server, addr) {
			b = proxyBackup{}
		}
		s.backup = b
		s.hasBack = true
	}
	s.addr = addr
	// Взводим флаг до записи: если один из SetValue упадёт на полпути, реестр уже
	// изменён, и Clear обязан его вычистить.
	s.active = true

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", proxyBypass); err != nil {
		return err
	}

	notifyWinInet()
	return nil
}

// Clear возвращает системный прокси к состоянию до включения.
// Безопасно вызывать многократно и когда прокси не был установлен.
func (s *SystemProxy) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active && !s.hasBack {
		return nil // в этом сеансе прокси не ставили — чужие настройки не трогаем
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

// ClearStale снимает системный прокси, оставшийся от прошлого запуска.
// Если приложение убили (краш, taskkill, выключение питания) при активном
// прокси, в реестре остаётся наш адрес — а слушать его уже некому, и у
// пользователя «пропадает интернет» до ручной правки настроек Windows.
// Трогаем реестр только когда там стоит именно наш адрес.
func (s *SystemProxy) ClearStale(addr string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active || addr == "" {
		return false, nil // прокси наш и живой — это не остаток
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, proxyRegPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("открытие ключа реестра: %w", err)
	}
	defer k.Close()

	cur := readBackup(k)
	if cur.enable == 0 || !sameProxyAddr(cur.server, addr) {
		return false, nil
	}
	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		return false, err
	}
	notifyWinInet()
	return true, nil
}

// Active сообщает, включён ли сейчас системный прокси нами.
func (s *SystemProxy) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// sameProxyAddr сравнивает значение ProxyServer с нашим адресом. Windows пишет
// туда либо "host:port", либо список вида "http=host:port;https=host:port" —
// нашим считаем адрес, встретившийся в любой схеме.
func sameProxyAddr(server, addr string) bool {
	if server == "" || addr == "" {
		return false
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	for _, part := range strings.Split(strings.ToLower(server), ";") {
		part = strings.TrimSpace(part)
		if i := strings.Index(part, "="); i >= 0 {
			part = part[i+1:]
		}
		if part == addr {
			return true
		}
	}
	return false
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
//
// NewLazySystemDLL, а не syscall.NewLazyDLL: последний идёт обычным порядком
// поиска, где каталог приложения стоит впереди System32. wininet.dll в список
// KnownDLLs не входит, то есть подложенная рядом с exe копия загрузилась бы
// вместо системной — а при TUN мы работаем с правами администратора из
// портативного каталога, куда пишет любой пользователь. Системный вариант
// заставляет LOAD_LIBRARY_SEARCH_SYSTEM32 и закрывает подмену.
func notifyWinInet() {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	const (
		INTERNET_OPTION_SETTINGS_CHANGED = 39
		INTERNET_OPTION_REFRESH          = 37
	)
	setOption.Call(0, INTERNET_OPTION_SETTINGS_CHANGED, 0, 0)
	setOption.Call(0, INTERNET_OPTION_REFRESH, 0, 0)
}
