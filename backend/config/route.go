package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"Proxy/backend/rules"
)

// routeBuilder переводит пользовательские правила (backend/rules) в route-правила
// sing-box. Он же собирает список локальных rule-set'ов, на которые эти правила
// ссылаются, — в конфиг попадают только реально используемые .srs.
type routeBuilder struct {
	opts     Options
	nodeTags []string        // теги нод текущего профиля (для групп)
	groups   map[string]bool // имена групп, которые реально попали в outbounds
	hasProxy bool            // есть ли outbound "proxy" (иначе proxy-правила бессмысленны)
	setsUsed map[string]bool // теги rule-set'ов, уже добавленных в конфиг
	ruleSets []ruleSet
}

// buildRoute собирает правила маршрутизации и список локальных rule-set'ов.
//
// Порядок: служебные правила (sniff, перехват DNS, отсечение QUIC в TUN) идут
// первыми и не настраиваются, дальше — пользовательский список сверху вниз.
// Первое совпавшее правило выигрывает, как и в самом sing-box.
func buildRoute(opts Options, nodeTags []string, groupNames map[string]bool) ([]json.RawMessage, []ruleSet, error) {
	b := &routeBuilder{
		opts:     opts,
		nodeTags: nodeTags,
		groups:   groupNames,
		hasProxy: len(nodeTags) > 0,
		setsUsed: map[string]bool{},
	}

	var rulesJSON []json.RawMessage
	if err := appendJSON(&rulesJSON,
		map[string]interface{}{"action": "sniff"},
		map[string]interface{}{"protocol": "dns", "action": "hijack-dns"},
	); err != nil {
		return nil, nil, err
	}

	// Режимы Clash API. Правила ловят режим по clash_mode и перекрывают весь
	// пользовательский список — поэтому стоят до него. Переключение режима идёт
	// через PATCH /configs и не требует перезапуска ядра.
	if len(nodeTags) > 0 {
		if err := appendJSON(&rulesJSON, map[string]interface{}{
			"clash_mode": ModeGlobal, "outbound": ProxyTag,
		}); err != nil {
			return nil, nil, err
		}
	}
	if err := appendJSON(&rulesJSON, map[string]interface{}{
		"clash_mode": ModeDirect, "outbound": DirectTag,
	}); err != nil {
		return nil, nil, err
	}
	// Ядро принимает только те режимы, которые встретились в конфиге. Без этого
	// холостого правила режим Rule существует лишь тогда, когда он же стоит в
	// default_mode — и пользователь, переключившийся в Global, не может вернуться
	// к правилам без перезапуска ядра. Действием берём повторный sniff: он не
	// прерывает разбор и ничего не меняет (трафик уже разобран первым правилом),
	// а пустое route-options ядро отвергает.
	if err := appendJSON(&rulesJSON, map[string]interface{}{
		"clash_mode": ModeRule, "action": "sniff",
	}); err != nil {
		return nil, nil, err
	}

	// Режем QUIC в TUN: на TCP-нодах UDP:443 не проходит, и браузер зависает на
	// HTTP/3 вместо fallback на TCP (ломаются Google/YouTube/медиа). reject
	// заставляет клиента сразу перейти на HTTP/2 поверх TCP.
	if opts.EnableTUN && opts.BlockQUIC {
		if err := appendJSON(&rulesJSON, map[string]interface{}{
			"protocol": "quic", "action": "reject",
		}); err != nil {
			return nil, nil, err
		}
	}

	for _, r := range opts.Routing.EnabledRules() {
		raw, ok, err := b.convert(r)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			rulesJSON = append(rulesJSON, raw)
		}
	}

	return rulesJSON, b.ruleSets, nil
}

// convert превращает одно правило в route-правило sing-box. ok=false означает,
// что правило неприменимо в текущем окружении (нет прокси, отсутствует .srs) —
// такое правило молча пропускается, трафик уходит по следующим правилам.
func (b *routeBuilder) convert(r rules.Rule) (json.RawMessage, bool, error) {
	m := map[string]interface{}{}

	vals := rules.CleanValues(r.Match, r.Values)
	switch r.Match {
	case rules.MatchPrivate:
		m["ip_is_private"] = true
	case rules.MatchRuleSet:
		var tags []string
		for _, tag := range vals {
			if !b.addRuleSet(tag) {
				continue // .srs не найден — правило без него бессмысленно
			}
			tags = append(tags, tag)
		}
		if len(tags) == 0 {
			return nil, false, nil
		}
		m["rule_set"] = tags
	case rules.MatchPort:
		var ports []int
		for _, v := range vals {
			var p int
			if _, err := fmt.Sscanf(v, "%d", &p); err != nil {
				return nil, false, fmt.Errorf("правило %q: некорректный порт %q", r.Title(), v)
			}
			ports = append(ports, p)
		}
		m["port"] = ports
	default:
		field, ok := matchFields[r.Match]
		if !ok {
			return nil, false, fmt.Errorf("правило %q: неизвестный тип совпадения %q", r.Title(), r.Match)
		}
		if len(vals) == 0 {
			return nil, false, nil
		}
		m[field] = vals
	}

	switch r.Action {
	case rules.ActionBlock:
		m["action"] = "reject"
		if r.Method == rules.RejectDrop {
			// drop вместо ICMP/RST: клиент не получает отказ и уходит в таймаут.
			// Нужно там, где явный отказ ломает fallback (напр. QUIC в браузере).
			m["method"] = "drop"
		}
	case rules.ActionDirect:
		m["outbound"] = DirectTag
	case rules.ActionProxy:
		if !b.hasProxy {
			return nil, false, nil // нод нет — направлять некуда
		}
		out := ProxyTag
		if r.Target != "" && b.groups[r.Target] {
			out = r.Target
		}
		m["outbound"] = out
	default:
		return nil, false, fmt.Errorf("правило %q: неизвестное действие %q", r.Title(), r.Action)
	}

	// tls_fragment — поле действия route, поэтому его приходится указывать явно
	// (по умолчанию action выводится из наличия outbound).
	if r.TLSFragment && r.Action != rules.ActionBlock {
		m["action"] = "route"
		m["tls_fragment"] = true
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// matchFields — соответствие типов совпадения полям route-правила sing-box.
var matchFields = map[string]string{
	rules.MatchDomain:        "domain",
	rules.MatchDomainSuffix:  "domain_suffix",
	rules.MatchDomainKeyword: "domain_keyword",
	rules.MatchDomainRegex:   "domain_regex",
	rules.MatchIPCIDR:        "ip_cidr",
	rules.MatchProcess:       "process_name",
	rules.MatchProcessPath:   "process_path",
	rules.MatchProtocol:      "protocol",
	rules.MatchNetwork:       "network",
}

// addRuleSet регистрирует набор правил под тегом. Удалённые наборы описаны в
// routing.json, локальные ищутся файлом в каталоге ассетов.
//
// Возвращает false, если набор не найден: с несуществующим rule_set ядро не
// стартует, поэтому такие правила лучше выбросить, чем уронить подключение.
func (b *routeBuilder) addRuleSet(tag string) bool {
	if b.setsUsed[tag] {
		return true
	}
	if set := b.opts.Routing.FindRuleSet(tag); set != nil && set.Type == rules.SetRemote {
		// Качать можно и через прокси — для списков, заблокированных у провайдера.
		// Но если нод нет, единственный работающий detour — direct.
		detour := DirectTag
		if set.Detour == rules.ActionProxy && b.hasProxy {
			detour = ProxyTag
		}
		b.setsUsed[tag] = true
		b.ruleSets = append(b.ruleSets, ruleSet{
			Type: "remote", Tag: tag, Format: set.FormatOrDefault(),
			URL: set.URL, DownloadDetour: detour, UpdateInterval: set.UpdateInterval(),
		})
		return true
	}
	if b.opts.RuleSetDir == "" {
		return false
	}
	path := filepath.Join(b.opts.RuleSetDir, tag+".srs")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	b.setsUsed[tag] = true
	b.ruleSets = append(b.ruleSets, ruleSet{
		Type: "local", Tag: tag, Format: "binary", Path: path,
	})
	return true
}
