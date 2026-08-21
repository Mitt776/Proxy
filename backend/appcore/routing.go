package appcore

import (
	"fmt"

	"Proxy/backend/config"
	"Proxy/backend/rules"
	"Proxy/backend/settings"
)

// ConfigOptions собирает параметры генерации конфига из текущего состояния
// приложения: настройки, правила маршрутизации и пути к ассетам.
func (c *Core) ConfigOptions(nodes []config.Node, enableTUN bool) config.Options {
	var cr settings.Settings
	if c.settings != nil {
		cr = c.settings.Get()
	}
	var routing rules.Config
	if c.rules != nil {
		routing = c.rules.Get()
	}
	opts := config.Options{
		MixedPort:    c.mixedPort,
		ClashAPIPort: c.clashPort,
		ClashSecret:  c.clashSecret,
		LogLevel:     normalizeLogLevel(cr.LogLevel),
		EnableTUN:    enableTUN,
		Nodes:        nodes,
		Routing:      routing,
		Mode:         cr.Mode,
		BlockQUIC:    !cr.AllowQUIC,
		CacheDBPath:  "cache.db",
		// На Windows список всегда пуст: приложения «мимо VPN» существуют только
		// там, где туннель выдаёт VpnService.
		ExcludePackage: cr.ExcludedApps,
	}
	if c.paths != nil {
		opts.RuleSetDir = c.paths.AssetsDir
		opts.ListSetDir = c.ListSetDir()
		opts.GeoIPPath = c.paths.GeoIP
		opts.GeoSitePath = c.paths.GeoSite
	}
	return opts
}

// GetRouting возвращает весь список правил и групп для UI.
func (c *Core) GetRouting() rules.Config {
	if c.rules == nil {
		return rules.Default()
	}
	return c.rules.Get()
}

// withRouting меняет правила и сразу применяет их к живому ядру. Если ядро
// отвергло новый конфиг (или не смогло перезапуститься), правки откатываются:
// иначе на диске осталось бы правило, о котором ядро не знает, — UI показывает
// одно, трафик идёт по-другому, и разобраться в этом пользователю нечем.
func (c *Core) withRouting(mutate func() error) error {
	if c.rules == nil {
		return CodedErr(ErrNotReady, "правила не готовы")
	}
	prev := c.rules.Get() // глубокая копия — состояние до правки
	if err := mutate(); err != nil {
		return err
	}
	if err := c.ApplyRouting(); err != nil {
		if rerr := c.rules.Replace(prev); rerr != nil {
			return fmt.Errorf("%w (откатить правила не удалось: %v)", err, rerr)
		}
		return err
	}
	return nil
}

// SetRouting заменяет список правил целиком (drag-n-drop в UI) и применяет
// изменения к работающему ядру.
func (c *Core) SetRouting(cfg rules.Config) error {
	return c.withRouting(func() error { return c.rules.Replace(cfg) })
}

// AddRule добавляет правило в конец списка и возвращает его ID.
func (c *Core) AddRule(r rules.Rule) (string, error) {
	var id string
	err := c.withRouting(func() error {
		var e error
		id, e = c.rules.AddRule(r)
		return e
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateRule сохраняет изменённое правило.
func (c *Core) UpdateRule(r rules.Rule) error {
	return c.withRouting(func() error { return c.rules.UpdateRule(r) })
}

// DeleteRule удаляет правило (встроенные удалить нельзя — только выключить).
func (c *Core) DeleteRule(id string) error {
	return c.withRouting(func() error { return c.rules.DeleteRule(id) })
}

// MoveRule переставляет правило на позицию index — порядок задаёт приоритет.
func (c *Core) MoveRule(id string, index int) error {
	return c.withRouting(func() error { return c.rules.MoveRule(id, index) })
}

// SetRuleEnabled включает или выключает правило.
func (c *Core) SetRuleEnabled(id string, enabled bool) error {
	return c.withRouting(func() error { return c.rules.SetRuleEnabled(id, enabled) })
}

// SetRoutingFinal задаёт судьбу трафика, не попавшего ни под одно правило:
// rules.ActionProxy (всё через прокси) или rules.ActionDirect (сплит-туннель).
func (c *Core) SetRoutingFinal(final string) error {
	return c.withRouting(func() error {
		return c.rules.Update(func(cfg *rules.Config) { cfg.Final = final })
	})
}

// AddGroup создаёт группу нод и возвращает её ID.
func (c *Core) AddGroup(g rules.Group) (string, error) {
	var id string
	err := c.withRouting(func() error {
		var e error
		id, e = c.rules.AddGroup(g)
		return e
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateGroup сохраняет изменённую группу (переименование тянет за собой ссылки
// правил).
func (c *Core) UpdateGroup(g rules.Group) error {
	return c.withRouting(func() error { return c.rules.UpdateGroup(g) })
}

// DeleteGroup удаляет группу; ссылавшиеся на неё правила переходят на основной
// селектор.
func (c *Core) DeleteGroup(id string) error {
	return c.withRouting(func() error { return c.rules.DeleteGroup(id) })
}

// ApplyRouting пересобирает конфиг с новыми правилами и перезапускает ядро, если
// оно работает. Перезапуск идёт через Runner.Restart — он не показывает
// промежуточное «остановлено», иначе с активного соединения слетел бы системный
// прокси и пользователь на секунду остался бы без интернета.
func (c *Core) ApplyRouting() error {
	if !c.running() {
		return nil // применится при следующем подключении
	}
	if c.profiles == nil {
		return nil
	}
	activeID := c.profiles.ActiveID()
	if activeID == "" {
		return nil
	}
	nodes, err := c.profiles.ResolveNodes(activeID)
	if err != nil {
		return err
	}
	enableTUN := c.settings != nil && c.settings.Get().EnableTUN
	cfg, err := config.Generate(c.ConfigOptions(nodes, enableTUN))
	if err != nil {
		return err
	}
	// Битый конфиг не должен ронять живое соединение: проверяем до перезапуска.
	if err := c.runner.Check(cfg); err != nil {
		return CodedErrf(ErrCoreCheck, "%w", err)
	}
	return c.runner.Restart(cfg)
}
