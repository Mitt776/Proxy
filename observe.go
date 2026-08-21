package main

// Наблюдаемость (1.3.0): активные соединения ядра и статическая проверка домена
// по правилам. Оба метода десктопные: вкладки «Трафик» на мобиле нет, а проверка
// домена делегируется команде `sing-box rule-set match`, то есть требует ядра
// отдельным процессом.
//
// Наборы правил и уровень журнала переехали в backend/appcore — они переносимы;
// здесь остались только делегаты, чтобы Wails видел прежний API.

import (
	"context"
	"sort"
	"strings"
	"time"

	"Proxy/backend/appcore"
	"Proxy/backend/config"
	"Proxy/backend/rules"
	"Proxy/backend/system"
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
	if a.core == nil || a.core.State() != "running" {
		return []ConnectionRow{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t, err := a.core.Clash().Connections(ctx)
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
	if a.core == nil {
		return codedErr(appcore.ErrCoreStopped, "ядро не запущено")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.core.Clash().CloseConnection(ctx, id)
}

// CloseAllConnections обрывает все соединения. Нужно после правки правил:
// уже открытые соединения продолжают идти по старому маршруту.
func (a *App) CloseAllConnections() error {
	if a.core == nil {
		return codedErr(appcore.ErrCoreStopped, "ядро не запущено")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.core.Clash().CloseAllConnections(ctx)
}

// CheckDomain отвечает на вопрос «куда пойдёт этот домен» до того, как по нему
// пойдёт трафик: прогоняет его по включённым правилам сверху вниз. Доменные
// условия и наборы .srs проверяет само ядро (`sing-box rule-set match`), чтобы
// семантика совпадала с боевой. Отдельным процессом, поэтому только на Windows.
func (a *App) CheckDomain(domain string) (config.DomainCheck, error) {
	if a.manager == nil {
		return config.DomainCheck{}, codedErr(appcore.ErrCoreStopped, "ядро недоступно")
	}
	opts := a.core.ConfigOptions(nil, false)
	return config.CheckDomain(opts, domain, a.manager.RuleSetMatch)
}

// --- Делегаты в appcore ---

// ListRuleSets возвращает описанные удалённые наборы правил.
func (a *App) ListRuleSets() []rules.RuleSet { return a.core.ListRuleSets() }

// AddRuleSet добавляет удалённый набор правил и возвращает его ID.
func (a *App) AddRuleSet(rs rules.RuleSet) (string, error) { return a.core.AddRuleSet(rs) }

// UpdateRuleSet заменяет описание набора.
func (a *App) UpdateRuleSet(rs rules.RuleSet) error { return a.core.UpdateRuleSet(rs) }

// DeleteRuleSet удаляет описание набора.
func (a *App) DeleteRuleSet(id string) error { return a.core.DeleteRuleSet(id) }

// RefreshRuleSet перекачивает список и возвращает число доменов.
func (a *App) RefreshRuleSet(id string) (int, error) { return a.core.RefreshRuleSet(id) }

// GetLogLevel возвращает текущий уровень журнала ядра.
func (a *App) GetLogLevel() string { return a.core.GetLogLevel() }

// SetLogLevel меняет уровень журнала (применяется перезапуском ядра).
func (a *App) SetLogLevel(level string) error { return a.core.SetLogLevel(level) }
