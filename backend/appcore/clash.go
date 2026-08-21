package appcore

// Clash API: ноды, режимы, задержка и статистика работающего ядра.

import (
	"context"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/settings"
)

// ProxiesView — состояние selector'а для UI: выбранная нода и все варианты.
type ProxiesView struct {
	Selector string      `json:"selector"`
	Now      string      `json:"now"`
	Nodes    []ProxyNode `json:"nodes"`
}

// ProxyNode — одна нода/группа в списке selector'а с последней задержкой.
type ProxyNode struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Delay int    `json:"delay"` // мс; 0 — не измерялось/недоступно
}

// GetProxies возвращает selector "proxy", выбранную ноду и её варианты с задержками.
func (c *Core) GetProxies() (*ProxiesView, error) {
	if c.clash == nil {
		return nil, CodedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	proxies, err := c.clash.Proxies(ctx)
	if err != nil {
		return nil, err
	}
	sel, ok := proxies[config.ProxyTag]
	if !ok {
		return nil, CodedErrf(ErrNoSelector, "селектор %q не найден (нет активных нод?)", config.ProxyTag)
	}
	view := &ProxiesView{Selector: config.ProxyTag, Now: sel.Now}
	for _, name := range sel.All {
		p := proxies[name]
		view.Nodes = append(view.Nodes, ProxyNode{
			Name:  name,
			Type:  p.Type,
			Delay: p.LastDelay(),
		})
	}
	return view, nil
}

// SelectNode переключает selector на выбранную ноду (без рестарта ядра).
func (c *Core) SelectNode(name string) error {
	if c.clash == nil {
		return CodedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return c.clash.SelectProxy(ctx, config.ProxyTag, name)
}

// TestDelay замеряет задержку одной ноды через ядро (мс).
func (c *Core) TestDelay(name string) (int, error) {
	if c.clash == nil {
		return 0, CodedErr(ErrClashNotReady, "clash api не готов")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 8*time.Second)
	defer cancel()
	return c.clash.Delay(ctx, name, "", 5000)
}

// GetMode возвращает текущий режим: Rule (работают правила), Global (всё через
// прокси) или Direct (всё напрямую). У живого ядра спрашиваем само ядро — режим мог
// поменяться из другого клиента Clash API.
func (c *Core) GetMode() string {
	if c.running() && c.clash != nil {
		ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
		defer cancel()
		if mode, err := c.clash.Mode(ctx); err == nil && mode != "" {
			return mode
		}
	}
	if c.settings != nil {
		if m := c.settings.Get().Mode; m != "" {
			return m
		}
	}
	return config.ModeRule
}

// SetMode переключает режим. На работающем ядре это делается через Clash API
// (PATCH /configs) — мгновенно и без разрыва соединений; перезапуск не нужен.
func (c *Core) SetMode(mode string) error {
	switch mode {
	case config.ModeRule, config.ModeGlobal, config.ModeDirect:
	default:
		return CodedErrf(ErrModeUnknown, "неизвестный режим: %q", mode)
	}
	// Сначала живому ядру, потом на диск: откажи ядро — и сохранённый режим
	// разошёлся бы с тем, по которому реально идёт трафик.
	if c.running() && c.clash != nil {
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		defer cancel()
		if err := c.clash.SetMode(ctx, mode); err != nil {
			return err
		}
	}
	if c.settings != nil {
		if err := c.settings.Update(func(s *settings.Settings) { s.Mode = mode }); err != nil {
			return err
		}
	}
	c.host.Emit("core:mode", mode)
	return nil
}

// StartStatsPoller раз в секунду опрашивает /connections и шлёт "core:stats"
// (скорость вверх/вниз, суммарные байты, число соединений). Скорость считаем как
// дельту суммарных счётчиков между опросами.
func (c *Core) StartStatsPoller() {
	c.statsMu.Lock()
	if c.statsCancel != nil {
		c.statsMu.Unlock()
		return // уже запущен
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.statsCancel = cancel
	c.statsMu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		var lastDown, lastUp int64
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t, err := c.clash.Connections(ctx)
				if err != nil {
					continue
				}
				var downSpeed, upSpeed int64
				if !first {
					downSpeed = nonNeg(t.DownloadTotal - lastDown)
					upSpeed = nonNeg(t.UploadTotal - lastUp)
				}
				lastDown, lastUp = t.DownloadTotal, t.UploadTotal
				first = false
				c.host.OnStats(downSpeed, upSpeed)
				c.host.Emit("core:stats", map[string]any{
					"downSpeed":   downSpeed,
					"upSpeed":     upSpeed,
					"downTotal":   t.DownloadTotal,
					"upTotal":     t.UploadTotal,
					"connections": len(t.Connections),
				})
			}
		}
	}()
}

// StopStatsPoller останавливает поллер статистики (если запущен).
func (c *Core) StopStatsPoller() {
	c.statsMu.Lock()
	if c.statsCancel != nil {
		c.statsCancel()
		c.statsCancel = nil
	}
	c.statsMu.Unlock()
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
