// Package settings хранит пользовательские настройки приложения на диске
// (settings.json рядом с профилями). Отделено от профилей, чтобы правки
// маршрута/автозапуска не трогали список нод.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings — сохраняемые настройки приложения.
type Settings struct {
	RoutingMode    string `json:"routingMode"`    // global | ru-direct
	BlockAds       bool   `json:"blockAds"`       // блок рекламных доменов
	EnableTUN      bool   `json:"enableTUN"`      // последний выбранный режим перехвата
	Autostart      bool   `json:"autostart"`      // запуск вместе с Windows
	MinimizeToTray bool   `json:"minimizeToTray"` // сворачивать в трей вместо закрытия
	CorePath       string `json:"corePath"`       // путь к альтернативному sing-box.exe (пусто = встроенное ядро)

	SubUpdateHours int `json:"subUpdateHours"` // автообновление подписок каждые N часов (0 = выкл)

	// AllowQUIC: разрешить QUIC/HTTP-3 в TUN. По умолчанию (false) QUIC режется —
	// иначе на TCP-нодах (vless-vision, xhttp) UDP:443 уходит в чёрную дыру и
	// ломаются Google/YouTube/медиа. Инверсия сделана ради нулевого значения:
	// старые settings.json без поля → false → QUIC режется (безопасный дефолт).
	AllowQUIC bool `json:"allowQuic"`

	// Свои правила маршрутизации (домены, по одному в списке) поверх пресетов.
	DirectDomains []string `json:"directDomains"` // всегда напрямую
	ProxyDomains  []string `json:"proxyDomains"`  // всегда через прокси
	BlockDomains  []string `json:"blockDomains"`  // блокировать
}

// Defaults возвращает настройки по умолчанию.
func Defaults() Settings {
	return Settings{
		RoutingMode:    "global",
		BlockAds:       false,
		EnableTUN:      false,
		Autostart:      false,
		MinimizeToTray: true,
	}
}

// Store — потокобезопасное файловое хранилище настроек.
type Store struct {
	path string
	mu   sync.Mutex
	data Settings
}

// Load читает настройки (или создаёт со значениями по умолчанию).
func Load(dataDir string) (*Store, error) {
	s := &Store{
		path: filepath.Join(dataDir, "settings.json"),
		data: Defaults(),
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	// Повреждённый файл не роняет приложение — откатываемся к дефолтам.
	_ = json.Unmarshal(b, &s.data)
	return s, nil
}

// Get возвращает копию текущих настроек.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}

// Update применяет изменения через колбэк и атомарно сохраняет файл.
func (s *Store) Update(fn func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	return s.save()
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path) // атомарная замена
}
