// Package rules хранит пользовательские правила маршрутизации и группы нод.
//
// Пакет намеренно ничего не знает ни о sing-box, ни о Wails: это только модель
// данных + валидация + файловое хранилище (data\routing.json). Преобразование
// правил в route-правила ядра живёт в backend/config — там всё знание о схеме
// sing-box.
package rules

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Version — версия схемы routing.json. Растёт при несовместимых изменениях.
const Version = 1

// Действия правила.
const (
	ActionProxy  = "proxy"  // через прокси (основной селектор или группу из Target)
	ActionDirect = "direct" // напрямую, в обход прокси
	ActionBlock  = "block"  // отбросить соединение
)

// Типы совпадения. Значения соответствуют полям route-правил sing-box,
// но названы в camelCase, потому что уходят во фронтенд как есть.
const (
	MatchDomainSuffix  = "domainSuffix"  // домен и все поддомены
	MatchDomain        = "domain"        // точное совпадение домена
	MatchDomainKeyword = "domainKeyword" // подстрока в домене
	MatchDomainRegex   = "domainRegex"   // регулярное выражение по домену
	MatchIPCIDR        = "ipCIDR"        // IP или подсеть назначения
	MatchPort          = "port"          // порт назначения
	MatchProcess       = "process"       // имя процесса (chrome.exe)
	MatchProcessPath   = "processPath"   // полный путь к исполняемому файлу
	MatchRuleSet       = "ruleSet"       // локальный .srs (geoip-ru, geosite-ru, geosite-ads)
	MatchPrivate       = "private"       // приватные сети (LAN, роутер) — без значений
	MatchProtocol      = "protocol"      // определённый sniff-ом протокол (quic, dns, http, tls)
	MatchNetwork       = "network"       // tcp | udp
)

// Способы отбрасывания для ActionBlock.
const (
	// RejectDefault — вежливый отказ (ICMP unreachable / TCP RST): приложение
	// сразу видит ошибку и не ждёт таймаута.
	RejectDefault = ""
	// RejectDrop — молча выбросить пакет. Нужен для QUIC: браузер должен
	// «не заметить» UDP:443 и откатиться на TCP, а не получить явный отказ.
	RejectDrop = "drop"
)

// Типы групп нод.
const (
	GroupSelect  = "select"  // ручной выбор ноды (selector)
	GroupURLTest = "urltest" // автоподбор по задержке
)

// Rule — одно правило маршрутизации. Порядок в списке = приоритет:
// срабатывает первое подходящее, как в sing-box.
type Rule struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`    // подпись для UI; пустая — генерируем из значений
	Enabled bool     `json:"enabled"` // выключенные правила не попадают в конфиг
	Match   string   `json:"match"`   // один из Match*
	Values  []string `json:"values"`  // значения матчера (для MatchPrivate пусто)
	Action  string   `json:"action"`  // один из Action*
	Target  string   `json:"target"`  // ActionProxy: имя группы; пусто — основной селектор

	// Method — способ отказа для ActionBlock (см. Reject*).
	Method string `json:"method,omitempty"`
	// TLSFragment — резать TLS ClientHello на фрагменты для этого правила
	// (обход DPI по SNI). Только с ActionDirect: ядро режет полезную нагрузку,
	// которую пишет в outbound, поэтому у прокси-правила разрез остаётся внутри
	// туннеля и до провайдера не доезжает — см. config/route.go.
	TLSFragment bool `json:"tlsFragment,omitempty"`

	// Builtin — правило создано миграцией или дефолтами. Удалять из UI нельзя
	// (можно выключить или переставить), чтобы пользователь случайно не остался
	// без доступа к LAN.
	Builtin bool `json:"builtin,omitempty"`
}

// Group — группа нод, отдельный outbound-селектор. Позволяет слать разные
// правила через разные ноды («торренты → нода NL, всё остальное → авто»).
type Group struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`   // он же тег outbound-а, виден в Clash API
	Type   string   `json:"type"`   // GroupSelect | GroupURLTest
	Filter string   `json:"filter"` // регулярка по имени ноды; пусто — берём Nodes
	Nodes  []string `json:"nodes"`  // явный список тегов нод (если Filter пуст)
}

// Config — содержимое data\routing.json.
type Config struct {
	Version int     `json:"version"`
	Rules   []Rule  `json:"rules"`
	Groups  []Group `json:"groups"`
	// RuleSets — описания удалённых наборов правил (локальные .srs описывать
	// не нужно, они берутся из каталога ассетов по имени файла).
	RuleSets []RuleSet `json:"ruleSets,omitempty"`
	// Final — куда идёт трафик, не попавший ни под одно правило.
	Final string `json:"final"` // ActionProxy | ActionDirect
}

// Зарезервированные ядром теги outbound-ов — группа не может так называться.
var reservedTags = map[string]bool{
	"proxy": true, "auto": true, "direct": true, "block": true, "dns-out": true,
}

// Validate проверяет конфиг целиком: типы, значения, ссылки правил на группы.
func (c *Config) Validate() error {
	if c.Final != ActionProxy && c.Final != ActionDirect {
		return fmt.Errorf("final: ожидалось %q или %q, получено %q", ActionProxy, ActionDirect, c.Final)
	}
	names := map[string]bool{}
	for i := range c.Groups {
		g := &c.Groups[i]
		if err := g.Validate(); err != nil {
			return fmt.Errorf("группа %q: %w", g.Name, err)
		}
		if names[g.Name] {
			return fmt.Errorf("группа %q: имя уже занято", g.Name)
		}
		names[g.Name] = true
	}
	tags := map[string]bool{}
	for i := range c.RuleSets {
		rs := &c.RuleSets[i]
		if err := rs.Validate(); err != nil {
			return fmt.Errorf("набор правил %q: %w", rs.Tag, err)
		}
		if tags[rs.Tag] {
			return fmt.Errorf("набор правил %q: имя уже занято", rs.Tag)
		}
		tags[rs.Tag] = true
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		if err := r.Validate(); err != nil {
			return fmt.Errorf("правило %q: %w", r.Title(), err)
		}
		if r.Action == ActionProxy && r.Target != "" && !names[r.Target] {
			return fmt.Errorf("правило %q: группа %q не существует", r.Title(), r.Target)
		}
	}
	return nil
}

// Validate проверяет группу.
func (g *Group) Validate() error {
	if strings.TrimSpace(g.Name) == "" {
		return fmt.Errorf("пустое имя")
	}
	if reservedTags[strings.ToLower(strings.TrimSpace(g.Name))] {
		return fmt.Errorf("имя зарезервировано ядром")
	}
	switch g.Type {
	case GroupSelect, GroupURLTest:
	default:
		return fmt.Errorf("неизвестный тип %q", g.Type)
	}
	if g.Filter != "" {
		if _, err := regexp.Compile(g.Filter); err != nil {
			return fmt.Errorf("некорректный фильтр: %w", err)
		}
	}
	return nil
}

// MatchNodes выбирает теги нод, попадающих в группу: по регулярке (если задана)
// либо по явному списку. Порядок сохраняется как в профиле.
func (g *Group) MatchNodes(all []string) []string {
	if g.Filter != "" {
		re, err := regexp.Compile(g.Filter)
		if err != nil {
			return nil
		}
		var out []string
		for _, tag := range all {
			if re.MatchString(tag) {
				out = append(out, tag)
			}
		}
		return out
	}
	want := map[string]bool{}
	for _, n := range g.Nodes {
		want[n] = true
	}
	var out []string
	for _, tag := range all {
		if want[tag] {
			out = append(out, tag)
		}
	}
	return out
}

// Title — человекочитаемое имя правила (для UI и текстов ошибок).
func (r *Rule) Title() string {
	if strings.TrimSpace(r.Name) != "" {
		return r.Name
	}
	if r.Match == MatchPrivate {
		return "Приватные сети"
	}
	if len(r.Values) == 0 {
		return r.Match
	}
	if len(r.Values) > 3 {
		return fmt.Sprintf("%s и ещё %d", strings.Join(r.Values[:3], ", "), len(r.Values)-3)
	}
	return strings.Join(r.Values, ", ")
}

// Validate проверяет одно правило: известный матчер/действие и осмысленные значения.
func (r *Rule) Validate() error {
	switch r.Action {
	case ActionProxy, ActionDirect, ActionBlock:
	default:
		return fmt.Errorf("неизвестное действие %q", r.Action)
	}
	if r.Action == ActionBlock {
		if r.Method != RejectDefault && r.Method != RejectDrop {
			return fmt.Errorf("неизвестный способ отказа %q", r.Method)
		}
		if r.TLSFragment {
			return fmt.Errorf("фрагментация TLS несовместима с блокировкой")
		}
	} else if r.Method != "" {
		return fmt.Errorf("способ отказа применим только к блокировке")
	}
	if r.TLSFragment && r.Action != ActionDirect {
		return fmt.Errorf("фрагментация TLS работает только с действием «напрямую»")
	}

	if r.Match == MatchPrivate {
		return nil // матчер без значений
	}
	vals := CleanValues(r.Match, r.Values)
	if len(vals) == 0 {
		return fmt.Errorf("не задано ни одного значения")
	}
	for _, v := range vals {
		if err := validateValue(r.Match, v); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(match, v string) error {
	switch match {
	case MatchDomain, MatchDomainSuffix, MatchDomainKeyword:
		if strings.ContainsAny(v, " \t/") {
			return fmt.Errorf("значение %q не похоже на домен", v)
		}
	case MatchDomainRegex:
		if _, err := regexp.Compile(v); err != nil {
			return fmt.Errorf("некорректная регулярка %q: %w", v, err)
		}
	case MatchIPCIDR:
		if _, err := netip.ParsePrefix(v); err != nil {
			if _, err2 := netip.ParseAddr(v); err2 != nil {
				return fmt.Errorf("некорректный IP или подсеть %q", v)
			}
		}
	case MatchPort:
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("некорректный порт %q", v)
		}
	case MatchProcess, MatchProcessPath, MatchRuleSet:
		// произвольные строки — проверять нечего
	case MatchProtocol:
		switch v {
		case "quic", "dns", "http", "tls", "bittorrent", "stun", "ssh", "rdp", "dtls":
		default:
			return fmt.Errorf("неизвестный протокол %q", v)
		}
	case MatchNetwork:
		if v != "tcp" && v != "udp" {
			return fmt.Errorf("сеть должна быть tcp или udp, получено %q", v)
		}
	default:
		return fmt.Errorf("неизвестный тип совпадения %q", match)
	}
	return nil
}

// CleanValues нормализует значения матчера: убирает пробелы, пустые строки и
// дубли, домены приводит к нижнему регистру, голый IP дополняет до /32 (/128).
func CleanValues(match string, in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		switch match {
		case MatchDomain, MatchDomainSuffix, MatchDomainKeyword, MatchProtocol, MatchNetwork:
			v = strings.ToLower(v)
		case MatchIPCIDR:
			if addr, err := netip.ParseAddr(v); err == nil {
				v = fmt.Sprintf("%s/%d", addr.String(), addr.BitLen())
			}
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Normalize приводит конфиг в каноничный вид: чистит значения, проставляет
// версию и Final по умолчанию, выкидывает правила без значений.
func (c *Config) Normalize() {
	if c.Version == 0 {
		c.Version = Version
	}
	if c.Final != ActionDirect {
		c.Final = ActionProxy
	}
	out := c.Rules[:0]
	for _, r := range c.Rules {
		if r.Match != MatchPrivate {
			r.Values = CleanValues(r.Match, r.Values)
			if len(r.Values) == 0 {
				continue // пустое правило смысла не имеет
			}
		} else {
			r.Values = nil
		}
		if r.Action != ActionBlock {
			r.Method = ""
		}
		if r.Action != ActionDirect {
			// Флаг мог остаться в старом routing.json: до 2.0.0 UI разрешал
			// фрагментацию и на прокси-правилах, где она только добавляла
			// задержку. Гасим здесь, иначе строгий Validate уронил бы первое же
			// сохранение правил.
			r.TLSFragment = false
		}
		out = append(out, r)
	}
	c.Rules = out
	for i := range c.Groups {
		if c.Groups[i].Filter != "" {
			c.Groups[i].Nodes = nil // фильтр и явный список взаимоисключающи
		}
	}
	for i := range c.RuleSets {
		rs := &c.RuleSets[i]
		rs.Tag = strings.TrimSpace(rs.Tag)
		rs.URL = strings.TrimSpace(rs.URL)
		if rs.Type == "" {
			rs.Type = SetRemote // локальные наборы в конфиге не описывают
		}
	}
}

// EnabledRules возвращает только включённые правила в исходном порядке.
func (c *Config) EnabledRules() []Rule {
	var out []Rule
	for _, r := range c.Rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}
