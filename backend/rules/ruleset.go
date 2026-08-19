package rules

import (
	"fmt"
	"net/url"
	"strings"
)

// Типы наборов правил.
const (
	// SetLocal — файл .srs рядом с приложением (каталог ассетов). Тег совпадает
	// с именем файла без расширения.
	SetLocal = "local"
	// SetRemote — набор, который ядро скачивает само по URL и держит в cache.db.
	// Требует включённого experimental.cache_file (он у нас всегда включён).
	SetRemote = "remote"
)

// Форматы наборов правил sing-box.
const (
	FormatBinary = "binary" // скомпилированный .srs
	FormatSource = "source" // исходный JSON
)

// RuleSet — удалённый набор правил, на который можно сослаться из правила с
// матчером MatchRuleSet. Локальные наборы описывать не нужно: они берутся из
// каталога ассетов по имени файла.
type RuleSet struct {
	ID   string `json:"id"`
	Tag  string `json:"tag"`  // имя набора; на него ссылаются значения правила
	Type string `json:"type"` // SetLocal | SetRemote
	// URL — откуда качать (только для SetRemote).
	URL string `json:"url,omitempty"`
	// Format — binary для .srs, source для JSON. По умолчанию binary.
	Format string `json:"format,omitempty"`
	// UpdateHours — как часто обновлять; 0 = раз в сутки.
	UpdateHours int `json:"updateHours,omitempty"`
	// Detour — через что качать: ActionDirect (по умолчанию) или ActionProxy.
	// Для заблокированных источников (списки антицензуры) нужен прокси.
	Detour string `json:"detour,omitempty"`
}

// Validate проверяет описание набора.
func (rs *RuleSet) Validate() error {
	tag := strings.TrimSpace(rs.Tag)
	if tag == "" {
		return fmt.Errorf("пустое имя набора")
	}
	if strings.ContainsAny(tag, " \t/\\") {
		return fmt.Errorf("имя набора %q не должно содержать пробелов и слэшей", tag)
	}
	switch rs.Type {
	case SetRemote:
		u, err := url.Parse(strings.TrimSpace(rs.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("нужен http(s)-адрес набора, получено %q", rs.URL)
		}
	case SetLocal:
		// путь резолвится в backend/config по каталогу ассетов
	default:
		return fmt.Errorf("неизвестный тип набора %q", rs.Type)
	}
	switch rs.Format {
	case "", FormatBinary, FormatSource:
	default:
		return fmt.Errorf("неизвестный формат %q (ожидался %q или %q)", rs.Format, FormatBinary, FormatSource)
	}
	switch rs.Detour {
	case "", ActionDirect, ActionProxy:
	default:
		return fmt.Errorf("неизвестный способ загрузки %q", rs.Detour)
	}
	if rs.UpdateHours < 0 || rs.UpdateHours > 24*30 {
		return fmt.Errorf("интервал обновления вне разумных границ: %d ч", rs.UpdateHours)
	}
	return nil
}

// FormatOrDefault возвращает формат набора с учётом значения по умолчанию.
func (rs *RuleSet) FormatOrDefault() string {
	if rs.Format == FormatSource {
		return FormatSource
	}
	return FormatBinary
}

// UpdateInterval возвращает интервал обновления в виде строки sing-box ("24h").
func (rs *RuleSet) UpdateInterval() string {
	h := rs.UpdateHours
	if h <= 0 {
		h = 24
	}
	return fmt.Sprintf("%dh", h)
}

// FindRuleSet возвращает описание набора по тегу (nil, если не описан — значит
// он локальный и ищется в каталоге ассетов).
func (c *Config) FindRuleSet(tag string) *RuleSet {
	for i := range c.RuleSets {
		if c.RuleSets[i].Tag == tag {
			return &c.RuleSets[i]
		}
	}
	return nil
}

// RuleSetUsedBy возвращает названия правил, которые ссылаются на набор.
func (c *Config) RuleSetUsedBy(tag string) []string {
	var out []string
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Match != MatchRuleSet {
			continue
		}
		for _, v := range r.Values {
			if v == tag {
				out = append(out, r.Title())
				break
			}
		}
	}
	return out
}
