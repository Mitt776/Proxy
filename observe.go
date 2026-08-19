package main

// Наблюдаемость (1.3.0): активные соединения ядра, статическая проверка домена
// по правилам, удалённые наборы правил и уровень журнала. Всё это — публичные
// методы App, то есть API для фронтенда.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
	"Proxy/backend/rules"
	"Proxy/backend/settings"
	"Proxy/backend/system"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ConnectionRow — активное соединение в том виде, в каком его показывает UI.
type ConnectionRow struct {
	ID string `json:"id"`
	// Host — домен из sniff-а, а если его нет — IP назначения.
	Host        string `json:"host"`
	DestIP      string `json:"destIP"`
	Port        string `json:"port"`
	Network     string `json:"network"` // tcp|udp
	Process     string `json:"process"`
	ProcessPath string `json:"processPath"`
	// Outbound — куда трафик ушёл в итоге (последнее звено цепочки ядра).
	Outbound string `json:"outbound"`
	Chain    string `json:"chain"`
	// Rule — сработавшее route-правило глазами ядра ("rule_set geosite-ru").
	Rule     string `json:"rule"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Seconds  int    `json:"seconds"` // сколько соединение живёт
}

// GetConnections возвращает активные соединения ядра, самые свежие сверху.
func (a *App) GetConnections() ([]ConnectionRow, error) {
	if a.clash == nil || a.manager == nil || a.manager.State() != core.StateRunning {
		return []ConnectionRow{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t, err := a.clash.Connections(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	out := make([]ConnectionRow, 0, len(t.Connections))
	for _, c := range t.Connections {
		m := c.Metadata
		host := m.Host
		if host == "" {
			host = m.DestinationIP
		}
		row := ConnectionRow{
			ID: c.ID, Host: host, DestIP: m.DestinationIP, Port: m.DestinationPort,
			Network: m.Network, Process: m.Process, ProcessPath: m.ProcessPath,
			Chain: strings.Join(c.Chains, " → "), Rule: strings.TrimSpace(c.Rule + " " + c.RulePayload),
			Upload: c.Upload, Download: c.Download,
		}
		// Цепочка ядра идёт от последнего звена к первому: outbound, через
		// который трафик реально ушёл, стоит в её конце.
		if len(c.Chains) > 0 {
			row.Outbound = c.Chains[len(c.Chains)-1]
		}
		if row.Process == "" && m.ProcessPath != "" {
			row.Process = system.ExeName(m.ProcessPath)
		}
		if !c.Start.IsZero() {
			row.Seconds = int(now.Sub(c.Start).Seconds())
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seconds < out[j].Seconds })
	return out, nil
}

// CloseConnection обрывает одно соединение.
func (a *App) CloseConnection(id string) error {
	if a.clash == nil {
		return fmt.Errorf("ядро не запущено")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.clash.CloseConnection(ctx, id)
}

// CloseAllConnections обрывает все соединения. Нужно после правки правил:
// уже открытые соединения продолжают идти по старому маршруту.
func (a *App) CloseAllConnections() error {
	if a.clash == nil {
		return fmt.Errorf("ядро не запущено")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.clash.CloseAllConnections(ctx)
}

// CheckDomain отвечает на вопрос «куда пойдёт этот домен» до того, как по нему
// пойдёт трафик: прогоняет его по включённым правилам сверху вниз. Доменные
// условия и наборы .srs проверяет само ядро (`sing-box rule-set match`), чтобы
// семантика совпадала с боевой.
func (a *App) CheckDomain(domain string) (config.DomainCheck, error) {
	if a.manager == nil {
		return config.DomainCheck{}, fmt.Errorf("ядро недоступно")
	}
	opts := a.configOptions(nil, false)
	return config.CheckDomain(opts, domain, a.manager.RuleSetMatch)
}

// ListRuleSets возвращает описанные удалённые наборы правил.
func (a *App) ListRuleSets() []rules.RuleSet {
	if a.rules == nil {
		return nil
	}
	return a.rules.Get().RuleSets
}

// AddRuleSet добавляет удалённый набор правил и возвращает его ID.
// Как и правки правил, идёт через withRouting: не принятый ядром набор
// откатывается, а не остаётся висеть в routing.json.
func (a *App) AddRuleSet(rs rules.RuleSet) (string, error) {
	var id string
	err := a.withRouting(func() error {
		var e error
		id, e = a.rules.AddRuleSet(rs)
		return e
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// UpdateRuleSet заменяет описание набора.
func (a *App) UpdateRuleSet(rs rules.RuleSet) error {
	return a.withRouting(func() error { return a.rules.UpdateRuleSet(rs) })
}

// DeleteRuleSet удаляет описание набора (если на него никто не ссылается).
func (a *App) DeleteRuleSet(id string) error {
	return a.withRouting(func() error { return a.rules.DeleteRuleSet(id) })
}

// LogLevels — уровни журнала ядра от самого подробного к самому тихому.
var LogLevels = []string{"trace", "debug", "info", "warn", "error"}

// GetLogLevel возвращает текущий уровень журнала ядра.
func (a *App) GetLogLevel() string {
	if a.settings == nil {
		return config.DefaultLogLevel
	}
	return normalizeLogLevel(a.settings.Get().LogLevel)
}

// SetLogLevel меняет уровень журнала. Уровень зашит в конфиг, поэтому на живом
// ядре он применяется перезапуском — как и правки правил, без снятия прокси.
func (a *App) SetLogLevel(level string) error {
	level = normalizeLogLevel(level)
	if a.settings == nil {
		return fmt.Errorf("настройки недоступны")
	}
	if err := a.settings.Update(func(s *settings.Settings) { s.LogLevel = level }); err != nil {
		return err
	}
	if err := a.applyRouting(); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "core:loglevel", level)
	return nil
}

// normalizeLogLevel приводит уровень к известному ядру значению.
func normalizeLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	for _, l := range LogLevels {
		if l == level {
			return level
		}
	}
	return config.DefaultLogLevel
}
