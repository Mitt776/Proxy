package main

// Наблюдаемость (1.3.0): активные соединения ядра, статическая проверка домена
// по правилам, удалённые наборы правил и уровень журнала. Всё это — публичные
// методы App, то есть API для фронтенда.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
		return codedErr(ErrCoreStopped, "ядро не запущено")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.clash.CloseConnection(ctx, id)
}

// CloseAllConnections обрывает все соединения. Нужно после правки правил:
// уже открытые соединения продолжают идти по старому маршруту.
func (a *App) CloseAllConnections() error {
	if a.clash == nil {
		return codedErr(ErrCoreStopped, "ядро не запущено")
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
		return config.DomainCheck{}, codedErr(ErrCoreStopped, "ядро недоступно")
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
	// Список качаем до записи в routing.json: битый адрес должен вернуть ошибку
	// сразу, а не превратиться в набор, который ядро молча проигнорирует.
	if err := a.syncListSet(rs); err != nil {
		return "", err
	}
	var id string
	err := a.withRouting(func() error {
		var e error
		id, e = a.rules.AddRuleSet(rs)
		return e
	})
	if err != nil {
		config.RemoveListSet(a.listSetDir(), rs.Tag)
		return "", err
	}
	return id, nil
}

// UpdateRuleSet заменяет описание набора.
func (a *App) UpdateRuleSet(rs rules.RuleSet) error {
	prev := a.findRuleSetByID(rs.ID)
	if err := a.syncListSet(rs); err != nil {
		return err
	}
	if err := a.withRouting(func() error { return a.rules.UpdateRuleSet(rs) }); err != nil {
		return err
	}
	// Тег или формат поменялись — старый файл больше никому не нужен.
	if prev != nil && prev.IsList() && (prev.Tag != rs.Tag || !rs.IsList()) {
		config.RemoveListSet(a.listSetDir(), prev.Tag)
	}
	return nil
}

// DeleteRuleSet удаляет описание набора (если на него никто не ссылается).
func (a *App) DeleteRuleSet(id string) error {
	prev := a.findRuleSetByID(id)
	if err := a.withRouting(func() error { return a.rules.DeleteRuleSet(id) }); err != nil {
		return err
	}
	if prev != nil && prev.IsList() {
		config.RemoveListSet(a.listSetDir(), prev.Tag)
	}
	return nil
}

// RefreshRuleSet перекачивает список по кнопке в UI и возвращает число доменов.
func (a *App) RefreshRuleSet(id string) (int, error) {
	rs := a.findRuleSetByID(id)
	if rs == nil {
		return 0, codedErr(ErrSetNotFound, "набор правил не найден")
	}
	if !rs.IsList() {
		return 0, codedErr(ErrSetNotList, "обновлять вручную можно только текстовые списки")
	}
	n, err := a.fetchListSet(*rs)
	if err != nil {
		return 0, err
	}
	// Набор мог не попасть в прошлый конфиг из-за отсутствия файла — после
	// удачной загрузки перегенерируем, чтобы правило заработало без переподключения.
	if err := a.applyRouting(); err != nil {
		return n, err
	}
	return n, nil
}

func (a *App) findRuleSetByID(id string) *rules.RuleSet {
	if a.rules == nil || id == "" {
		return nil
	}
	sets := a.rules.Get().RuleSets
	for i := range sets {
		if sets[i].ID == id {
			return &sets[i]
		}
	}
	return nil
}

// syncListSet качает список, если набор действительно списочный. Для обычных
// наборов — тихо ничего не делает, чтобы вызывающим не пришлось разбирать типы.
func (a *App) syncListSet(rs rules.RuleSet) error {
	if !rs.IsList() {
		return nil
	}
	_, err := a.fetchListSet(rs)
	return err
}

func (a *App) fetchListSet(rs rules.RuleSet) (int, error) {
	dir := a.listSetDir()
	if dir == "" {
		return 0, codedErr(ErrNotReady, "каталог данных недоступен")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	n, err := config.SyncListSet(ctx, rs, dir, a.listSetClient(rs))
	if err != nil {
		return 0, codedErrf(ErrSetFetch, "не удалось загрузить список %q: %w", rs.Tag, err)
	}
	return n, nil
}

// listSetClient выбирает, чем качать список. «Через прокси» имеет смысл только
// на живом ядре: до подключения локальный порт никто не слушает, и запрос
// упал бы вместо того, чтобы просто пойти напрямую.
func (a *App) listSetClient(rs rules.RuleSet) *http.Client {
	client := &http.Client{Timeout: 60 * time.Second}
	if rs.Detour != rules.ActionProxy || a.manager == nil || a.manager.State() != core.StateRunning {
		return client
	}
	u, err := url.Parse("http://" + a.proxyAddr())
	if err != nil {
		return client
	}
	client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	return client
}

// refreshListSets перекачивает списки, которых нет на диске или которые старше
// своего интервала. Зовётся из планировщика подписок — отдельный таймер ради
// этого заводить не за чем, периодичность там та же по смыслу.
//
// Конфиг намеренно не пересобирается: ядро читает локальный набор при старте, и
// применить обновление можно только перезапуском. Дёргать его в фоне, посреди
// чужого видеозвонка, ради суточного обновления списка — плохой обмен. Новое
// содержимое подхватится при следующем подключении или любой правке правил, а
// кому нужно сейчас — есть кнопка (RefreshRuleSet, там перезапуск осознанный).
func (a *App) refreshListSets() {
	if a.rules == nil {
		return
	}
	dir := a.listSetDir()
	if dir == "" {
		return
	}
	for _, rs := range a.rules.Get().RuleSets {
		if !rs.IsList() {
			continue
		}
		if fresh(config.ListSetPath(dir, rs.Tag), rs.UpdateHours) {
			continue
		}
		if n, err := a.fetchListSet(rs); err != nil {
			a.logLine(fmt.Sprintf("набор правил %s: %v", rs.Tag, err))
		} else {
			a.logLine(fmt.Sprintf("набор правил %s обновлён: %d записей", rs.Tag, n))
		}
	}
}

// logLine кладёт строку приложения в тот же поток, что и журнал ядра: фоновая
// докачка списков происходит без участия пользователя, и единственное место,
// где о ней можно узнать, — вкладка «Журнал».
func (a *App) logLine(line string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "core:log", time.Now().Format("2006-01-02 15:04:05")+" "+line)
}

// fresh сообщает, что файл на диске моложе интервала обновления.
func fresh(path string, updateHours int) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	h := updateHours
	if h <= 0 {
		h = 24
	}
	return time.Since(st.ModTime()) < time.Duration(h)*time.Hour
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
		return codedErr(ErrNotReady, "настройки недоступны")
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
