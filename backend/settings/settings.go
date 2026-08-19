// Package settings хранит пользовательские настройки приложения на диске
// (settings.json рядом с профилями). Отделено от профилей, чтобы правки
// маршрута/автозапуска не трогали список нод.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Settings — сохраняемые настройки приложения.
type Settings struct {
	RoutingMode    string `json:"routingMode"`    // устарело (до 1.2.0): global | ru-direct
	BlockAds       bool   `json:"blockAds"`       // устарело (до 1.2.0): блок рекламных доменов
	EnableTUN      bool   `json:"enableTUN"`      // последний выбранный режим перехвата
	Autostart      bool   `json:"autostart"`      // запуск вместе с Windows
	MinimizeToTray bool   `json:"minimizeToTray"` // сворачивать в трей вместо закрытия
	CorePath       string `json:"corePath"`       // путь к альтернативному sing-box.exe (пусто = встроенное ядро)

	SubUpdateHours int `json:"subUpdateHours"` // автообновление подписок каждые N часов (0 = выкл)

	// Mode — режим маршрутизации Clash API: Rule (работают правила), Global
	// (всё через прокси) или Direct (всё напрямую). Пустое значение = Rule,
	// поэтому старые settings.json подхватываются без миграции.
	Mode string `json:"mode"`

	// AllowQUIC: разрешить QUIC/HTTP-3 в TUN. По умолчанию (false) QUIC режется —
	// иначе на TCP-нодах (vless-vision, xhttp) UDP:443 уходит в чёрную дыру и
	// ломаются Google/YouTube/медиа. Инверсия сделана ради нулевого значения:
	// старые settings.json без поля → false → QUIC режется (безопасный дефолт).
	AllowQUIC bool `json:"allowQuic"`

	// LogLevel — уровень журнала ядра: trace|debug|info|warn|error.
	// Пустое значение = info (так же читаются файлы старых версий).
	LogLevel string `json:"logLevel"`

	// Наследие версий до 1.2.0: режим маршрутизации, блок рекламы и три плоских
	// списка доменов. Начиная с 1.2.0 всё это живёт единым упорядоченным списком
	// в data\routing.json (backend/rules). Поля остаются только для миграции при
	// первом запуске новой версии — приложение их больше не пишет.
	DirectDomains []string `json:"directDomains"` // устарело: всегда напрямую
	ProxyDomains  []string `json:"proxyDomains"`  // устарело: всегда через прокси
	BlockDomains  []string `json:"blockDomains"`  // устарело: блокировать
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
		return s, err // хранилище рабочее (с дефолтами), об ошибке сообщаем наверх
	}
	// Повреждённый файл не роняет приложение — откатываемся к дефолтам, а сам
	// файл отводим в сторону: иначе первое же сохранение затрёт его содержимое,
	// и разобраться, что там было, будет уже негде.
	if uerr := json.Unmarshal(b, &s.data); uerr != nil {
		s.data = Defaults()
		bad := s.path + ".bad"
		if rerr := os.Rename(s.path, bad); rerr != nil {
			return s, fmt.Errorf("settings.json повреждён: %w", uerr)
		}
		return s, fmt.Errorf("settings.json повреждён, сохранён как %s: %w", filepath.Base(bad), uerr)
	}
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
