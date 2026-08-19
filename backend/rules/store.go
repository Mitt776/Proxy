package rules

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Теги локальных rule-set'ов (файлы .srs в каталоге ассетов).
const (
	RuleSetGeoIPRU    = "geoip-ru"
	RuleSetGeositeRU  = "geosite-ru"
	RuleSetGeositeAds = "geosite-ads"
)

// idFallback нумерует идентификаторы, когда системный источник случайности
// недоступен: прежний фолбэк давал всем правилам один и тот же "id6", а по ID
// адресуются правка, перестановка и удаление — два одинаковых ID означают, что
// пользователь редактирует не то правило.
var idFallback atomic.Uint64

// NewID генерирует короткий идентификатор правила/группы.
func NewID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("id%x%x", time.Now().UnixNano(), idFallback.Add(1))
	}
	return hex.EncodeToString(b[:])
}

// Default — набор правил для чистой установки: приватные сети напрямую,
// заготовки «блок рекламы» и «РФ напрямую» выключены.
func Default() Config {
	return Migrate("global", false, nil, nil, nil)
}

// Migrate строит список правил из настроек старых версий (до 1.2.0), где были
// режим маршрутизации, флаг блокировки рекламы и три плоских списка доменов.
// Порядок повторяет прежнюю логику generator.go, чтобы поведение не изменилось:
// свои правила → реклама → приватные сети → сплит-туннель РФ.
func Migrate(routingMode string, blockAds bool, direct, proxy, block []string) Config {
	c := Config{Version: Version, Final: ActionProxy}

	addDomains := func(name string, domains []string, action string) {
		vals := CleanValues(MatchDomainSuffix, domains)
		if len(vals) == 0 {
			return
		}
		c.Rules = append(c.Rules, Rule{
			ID: NewID(), Name: name, Enabled: true,
			Match: MatchDomainSuffix, Values: vals, Action: action,
		})
	}
	addDomains("Свои: блокировка", block, ActionBlock)
	addDomains("Свои: напрямую", direct, ActionDirect)
	addDomains("Свои: через прокси", proxy, ActionProxy)

	c.Rules = append(c.Rules,
		Rule{
			ID: NewID(), Name: "Блокировка рекламы", Enabled: blockAds, Builtin: true,
			Match: MatchRuleSet, Values: []string{RuleSetGeositeAds}, Action: ActionBlock,
		},
		Rule{
			ID: NewID(), Name: "Приватные сети (LAN, роутер)", Enabled: true, Builtin: true,
			Match: MatchPrivate, Action: ActionDirect,
		},
		Rule{
			ID: NewID(), Name: "Россия — напрямую", Enabled: routingMode == "ru-direct", Builtin: true,
			Match: MatchRuleSet, Values: []string{RuleSetGeoIPRU, RuleSetGeositeRU}, Action: ActionDirect,
		},
	)
	return c
}

// Store — потокобезопасное файловое хранилище правил (data\routing.json).
type Store struct {
	path   string
	mu     sync.Mutex
	data   Config
	exists bool // файл был на диске при загрузке
}

// Load читает routing.json. Отсутствие файла — не ошибка: конфиг остаётся
// пустым, а вызывающая сторона решает, мигрировать со старых настроек или
// взять Default (см. Init).
func Load(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "routing.json")}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = Config{Version: Version, Final: ActionProxy}
			return s, nil
		}
		s.data = Config{Version: Version, Final: ActionProxy}
		return s, err // хранилище отдаём рабочим: без правил приложение живёт
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		// Повреждённый файл не должен ронять приложение: откатываемся к дефолту,
		// но помечаем как несуществующий — Init перезапишет его корректным.
		// Прежний файл отводим в сторону, чтобы правки пользователя не пропали
		// молча вместе с перезаписью.
		s.data = Default()
		bad := s.path + ".bad"
		if rerr := os.Rename(s.path, bad); rerr != nil {
			return s, fmt.Errorf("routing.json повреждён: %w", err)
		}
		return s, fmt.Errorf("routing.json повреждён, сохранён как %s: %w", filepath.Base(bad), err)
	}
	s.data.Normalize()
	s.exists = true
	return s, nil
}

// Exists сообщает, был ли файл правил на диске (иначе нужна миграция).
func (s *Store) Exists() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exists
}

// Init записывает начальный конфиг, если файла ещё не было. Повторный вызов
// ничего не делает.
func (s *Store) Init(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists {
		return nil
	}
	cfg.Normalize()
	s.data = cfg
	s.exists = true
	return s.save()
}

// Get возвращает глубокую копию конфига — вызывающий может её свободно менять.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.clone()
}

func (c Config) clone() Config {
	out := c
	out.Rules = make([]Rule, len(c.Rules))
	copy(out.Rules, c.Rules)
	for i := range out.Rules {
		out.Rules[i].Values = append([]string(nil), c.Rules[i].Values...)
	}
	out.Groups = make([]Group, len(c.Groups))
	copy(out.Groups, c.Groups)
	for i := range out.Groups {
		out.Groups[i].Nodes = append([]string(nil), c.Groups[i].Nodes...)
	}
	out.RuleSets = append([]RuleSet(nil), c.RuleSets...)
	return out
}

// Replace целиком заменяет конфиг после валидации.
func (s *Store) Replace(cfg Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = cfg
	s.exists = true
	return s.save()
}

// Update применяет изменения через колбэк; при ошибке валидации изменения
// откатываются, файл не трогается.
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.data.clone()
	fn(&next)
	next.Normalize()
	if err := next.Validate(); err != nil {
		return err
	}
	s.data = next
	s.exists = true
	return s.save()
}

// AddRule добавляет правило в конец списка и возвращает его ID.
func (s *Store) AddRule(r Rule) (string, error) {
	if r.ID == "" {
		r.ID = NewID()
	}
	err := s.Update(func(c *Config) { c.Rules = append(c.Rules, r) })
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// UpdateRule заменяет правило по ID, сохраняя его позицию и флаг Builtin.
func (s *Store) UpdateRule(r Rule) error {
	return s.Update(func(c *Config) {
		for i := range c.Rules {
			if c.Rules[i].ID == r.ID {
				r.Builtin = c.Rules[i].Builtin
				c.Rules[i] = r
				return
			}
		}
	})
}

// DeleteRule удаляет правило. Встроенные правила удалить нельзя — их можно
// только выключить (иначе легко остаться без доступа к LAN).
func (s *Store) DeleteRule(id string) error {
	var protected bool
	err := s.Update(func(c *Config) {
		for i := range c.Rules {
			if c.Rules[i].ID != id {
				continue
			}
			if c.Rules[i].Builtin {
				protected = true
				return
			}
			c.Rules = append(c.Rules[:i], c.Rules[i+1:]...)
			return
		}
	})
	if protected {
		return fmt.Errorf("встроенное правило нельзя удалить — его можно выключить")
	}
	return err
}

// MoveRule переставляет правило на позицию index (0 — в начало списка).
func (s *Store) MoveRule(id string, index int) error {
	return s.Update(func(c *Config) {
		from := -1
		for i := range c.Rules {
			if c.Rules[i].ID == id {
				from = i
				break
			}
		}
		if from < 0 {
			return
		}
		r := c.Rules[from]
		c.Rules = append(c.Rules[:from], c.Rules[from+1:]...)
		if index < 0 {
			index = 0
		}
		if index > len(c.Rules) {
			index = len(c.Rules)
		}
		c.Rules = append(c.Rules[:index], append([]Rule{r}, c.Rules[index:]...)...)
	})
}

// SetRuleEnabled включает/выключает правило.
func (s *Store) SetRuleEnabled(id string, enabled bool) error {
	return s.Update(func(c *Config) {
		for i := range c.Rules {
			if c.Rules[i].ID == id {
				c.Rules[i].Enabled = enabled
				return
			}
		}
	})
}

// AddGroup добавляет группу нод.
func (s *Store) AddGroup(g Group) (string, error) {
	if g.ID == "" {
		g.ID = NewID()
	}
	err := s.Update(func(c *Config) { c.Groups = append(c.Groups, g) })
	if err != nil {
		return "", err
	}
	return g.ID, nil
}

// UpdateGroup заменяет группу по ID. Если группа переименована, ссылки правил
// на неё обновляются, чтобы конфиг остался целостным.
func (s *Store) UpdateGroup(g Group) error {
	return s.Update(func(c *Config) {
		for i := range c.Groups {
			if c.Groups[i].ID != g.ID {
				continue
			}
			old := c.Groups[i].Name
			c.Groups[i] = g
			if old != g.Name {
				for j := range c.Rules {
					if c.Rules[j].Target == old {
						c.Rules[j].Target = g.Name
					}
				}
			}
			return
		}
	})
}

// DeleteGroup удаляет группу; правила, которые на неё ссылались, переводятся
// на основной селектор.
func (s *Store) DeleteGroup(id string) error {
	return s.Update(func(c *Config) {
		for i := range c.Groups {
			if c.Groups[i].ID != id {
				continue
			}
			name := c.Groups[i].Name
			c.Groups = append(c.Groups[:i], c.Groups[i+1:]...)
			for j := range c.Rules {
				if c.Rules[j].Target == name {
					c.Rules[j].Target = ""
				}
			}
			return
		}
	})
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

// AddRuleSet добавляет описание удалённого набора правил.
func (s *Store) AddRuleSet(rs RuleSet) (string, error) {
	if rs.ID == "" {
		rs.ID = NewID()
	}
	err := s.Update(func(c *Config) { c.RuleSets = append(c.RuleSets, rs) })
	if err != nil {
		return "", err
	}
	return rs.ID, nil
}

// UpdateRuleSet заменяет набор по ID. При переименовании ссылки правил на старый
// тег переезжают на новый — иначе правило осталось бы без набора.
func (s *Store) UpdateRuleSet(rs RuleSet) error {
	return s.Update(func(c *Config) {
		for i := range c.RuleSets {
			if c.RuleSets[i].ID != rs.ID {
				continue
			}
			old := c.RuleSets[i].Tag
			c.RuleSets[i] = rs
			if old != rs.Tag {
				for j := range c.Rules {
					if c.Rules[j].Match != MatchRuleSet {
						continue
					}
					for k, v := range c.Rules[j].Values {
						if v == old {
							c.Rules[j].Values[k] = rs.Tag
						}
					}
				}
			}
			return
		}
	})
}

// DeleteRuleSet удаляет описание набора. Если на него ссылаются правила, удаление
// отклоняется: молча оставить правило с несуществующим набором значит тихо
// изменить маршрутизацию.
func (s *Store) DeleteRuleSet(id string) error {
	var used []string
	var tag string
	err := s.Update(func(c *Config) {
		for i := range c.RuleSets {
			if c.RuleSets[i].ID != id {
				continue
			}
			tag = c.RuleSets[i].Tag
			if used = c.RuleSetUsedBy(tag); len(used) > 0 {
				return
			}
			c.RuleSets = append(c.RuleSets[:i], c.RuleSets[i+1:]...)
			return
		}
	})
	if len(used) > 0 {
		return fmt.Errorf("набор %q используют правила: %s", tag, strings.Join(used, ", "))
	}
	return err
}
