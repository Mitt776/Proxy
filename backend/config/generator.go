package config

import (
	"encoding/json"
	"fmt"
	"strconv"

	"Proxy/backend/rules"
)

// Теги ключевых outbound-ов, на которые ссылаются route.final и Clash API.
const (
	ProxyTag  = "proxy"  // selector — то, что выбирает пользователь в UI
	AutoTag   = "auto"   // urltest — автоподбор лучшей ноды по задержке
	DirectTag = "direct" // прямое соединение
)

// Режимы Clash API. Пользователь переключает их на лету (PATCH /configs):
// правила маршрутизации работают только в ModeRule, остальные два — аварийные
// переключатели «всё через прокси» и «всё напрямую».
const (
	ModeRule   = "Rule"   // работают пользовательские правила
	ModeGlobal = "Global" // весь трафик через прокси
	ModeDirect = "Direct" // весь трафик напрямую (прокси остаётся поднятым)
)

// DefaultLogLevel — уровень журнала ядра, если пользователь не выбрал другой.
const DefaultLogLevel = "info"

// Defaults подставляет разумные значения в незаданные поля Options.
func (o *Options) Defaults() {
	if o.MixedPort == 0 {
		o.MixedPort = 2080
	}
	if o.ClashAPIPort == 0 {
		o.ClashAPIPort = 9090
	}
	if o.LogLevel == "" {
		o.LogLevel = DefaultLogLevel
	}
	if o.TUNStack == "" {
		o.TUNStack = "gvisor"
	}
}

// Generate собирает config.json sing-box и возвращает отформатированный JSON.
func Generate(opts Options) ([]byte, error) {
	opts.Defaults()

	nodeTags, nodeOutbounds, err := buildNodes(opts.Nodes)
	if err != nil {
		return nil, err
	}

	outbounds, groupNames, err := buildOutbounds(nodeTags, nodeOutbounds, opts.Routing.Groups)
	if err != nil {
		return nil, err
	}

	inbounds, err := buildInbounds(opts)
	if err != nil {
		return nil, err
	}

	// Прокси есть, только если в профиле есть ноды. Без них весь трафик идёт
	// напрямую — это позволяет проверить запуск ядра без реального сервера.
	hasProxy := len(nodeTags) > 0
	finalTag := DirectTag
	if hasProxy && opts.Routing.Final != rules.ActionDirect {
		finalTag = ProxyTag
	}

	routeRules, ruleSets, err := buildRoute(opts, nodeTags, groupNames)
	if err != nil {
		return nil, err
	}

	// Режим восстанавливаем из настроек: после перезапуска ядра пользователь
	// должен остаться в том же режиме, что выбрал.
	mode := opts.Mode
	switch mode {
	case ModeRule, ModeGlobal, ModeDirect:
	default:
		mode = ModeRule
	}

	cfg := singBoxConfig{
		Log:       logOptions{Level: opts.LogLevel, Timestamp: true},
		DNS:       buildDNS(hasProxy),
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Route: routeOptions{
			RuleSet:               ruleSets,
			Rules:                 routeRules,
			Final:                 finalTag,
			AutoDetectInterface:   true,
			DefaultDomainResolver: &domainResolver{Server: dnsLocalTag},
		},
		Experimental: experimental{
			ClashAPI: clashAPIOptions{
				ExternalController: "127.0.0.1:" + strconv.Itoa(opts.ClashAPIPort),
				Secret:             opts.ClashSecret,
				DefaultMode:        mode,
			},
			CacheFile: cacheFile{Enabled: true, Path: opts.CacheDBPath},
		},
	}

	return json.MarshalIndent(cfg, "", "  ")
}

// Теги DNS-серверов.
const (
	dnsRemoteTag = "dns-remote" // резолв через прокси (DoH) — без утечки DNS
	dnsLocalTag  = "dns-local"  // прямой резолв (для доменов серверов и direct-трафика)
)

// buildDNS собирает DNS-резолвер. Удалённый DNS ходит через прокси (DoH по TCP/443,
// проходит даже там, где UDP заблокирован, напр. VLESS+Vision), локальный — напрямую.
func buildDNS(hasProxy bool) dnsOptions {
	// У локального DNS detour не указываем: sing-box 1.12+ считает detour к пустому
	// direct-outbound бессмысленным (прямой резолв — поведение по умолчанию).
	local := dnsServer{Tag: dnsLocalTag, Type: "udp", Server: "223.5.5.5"}
	// ipv4_only: не отдаём AAAA. Иначе на нодах без IPv6-выхода приложения лезут
	// к Google/YouTube по IPv6 → чёрная дыра → «нет подключения к интернету».
	if !hasProxy {
		return dnsOptions{Servers: []dnsServer{local}, Final: dnsLocalTag, Strategy: "ipv4_only"}
	}
	remote := dnsServer{Tag: dnsRemoteTag, Type: "https", Server: "1.1.1.1", Detour: ProxyTag}
	return dnsOptions{
		Servers:  []dnsServer{remote, local},
		Final:    dnsRemoteTag,
		Strategy: "ipv4_only",
	}
}

// buildNodes дедуплицирует теги нод и возвращает упорядоченные теги + тела outbound-ов
// с проставленным (при необходимости уникализированным) тегом.
func buildNodes(nodes []Node) (tags []string, outbounds []json.RawMessage, err error) {
	seen := map[string]int{}
	for i, n := range nodes {
		tag := n.Tag
		if tag == "" {
			tag = "node-" + strconv.Itoa(i+1)
		}
		if c, ok := seen[tag]; ok {
			seen[tag] = c + 1
			tag = tag + " (" + strconv.Itoa(c+1) + ")"
		} else {
			seen[tag] = 1
		}

		// Гарантируем, что поле "tag" в теле outbound совпадает с нашим тегом.
		raw, err := setOutboundTag(n.Outbound, tag)
		if err != nil {
			return nil, nil, fmt.Errorf("нода %q: %w", tag, err)
		}
		tags = append(tags, tag)
		outbounds = append(outbounds, raw)
	}
	return tags, outbounds, nil
}

// setOutboundTag парсит тело outbound как объект и выставляет ему поле tag.
func setOutboundTag(raw json.RawMessage, tag string) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("некорректный JSON outbound: %w", err)
	}
	if _, ok := m["type"]; !ok {
		return nil, fmt.Errorf("у outbound отсутствует поле type")
	}
	tagJSON, _ := json.Marshal(tag)
	m["tag"] = tagJSON
	return json.Marshal(m)
}

// buildOutbounds формирует итоговый список outbound-ов и множество имён групп,
// которые реально попали в конфиг (на пустые группы правилам ссылаться нельзя).
// Если нод нет, остаётся один direct — это позволяет проверить запуск ядра без
// реального сервера.
func buildOutbounds(nodeTags []string, nodeOutbounds []json.RawMessage, groups []rules.Group) ([]json.RawMessage, map[string]bool, error) {
	var out []json.RawMessage
	names := map[string]bool{}

	if len(nodeTags) > 0 {
		// Группы идут в основной селектор перед отдельными нодами: так в UI
		// (и в Clash API) сначала видны осмысленные наборы, потом сырые ноды.
		var groupOutbounds []json.RawMessage
		var groupTags []string // порядок как в настройках пользователя
		for _, g := range groups {
			members := g.MatchNodes(nodeTags)
			if len(members) == 0 {
				continue // группа без нод сломала бы конфиг
			}
			names[g.Name] = true
			groupTags = append(groupTags, g.Name)
			var err error
			if g.Type == rules.GroupURLTest {
				err = appendJSON(&groupOutbounds, urltestOutbound{
					Type: "urltest", Tag: g.Name, Outbounds: members,
					URL: latencyTestURL, Interval: "3m",
				})
			} else {
				err = appendJSON(&groupOutbounds, selectorOutbound{
					Type: "selector", Tag: g.Name, Outbounds: members, Default: members[0],
				})
			}
			if err != nil {
				return nil, nil, err
			}
		}

		members := append([]string{AutoTag}, groupTags...)
		selector := selectorOutbound{
			Type:      "selector",
			Tag:       ProxyTag,
			Outbounds: append(members, nodeTags...),
			Default:   AutoTag,
		}
		urltest := urltestOutbound{
			Type:      "urltest",
			Tag:       AutoTag,
			Outbounds: nodeTags,
			URL:       latencyTestURL,
			Interval:  "3m",
		}
		if err := appendJSON(&out, selector, urltest); err != nil {
			return nil, nil, err
		}
		out = append(out, groupOutbounds...)
		out = append(out, nodeOutbounds...)
	}

	if err := appendJSON(&out, simpleOutbound{Type: "direct", Tag: DirectTag}); err != nil {
		return nil, nil, err
	}
	return out, names, nil
}

// latencyTestURL — эндпоинт для urltest: 204 без тела, отвечает быстро.
const latencyTestURL = "https://www.gstatic.com/generate_204"

// buildInbounds собирает mixed inbound и (опционально) tun inbound.
func buildInbounds(opts Options) ([]json.RawMessage, error) {
	var in []json.RawMessage

	mixed := mixedInbound{
		Type:       "mixed",
		Tag:        "mixed-in",
		Listen:     "127.0.0.1",
		ListenPort: opts.MixedPort,
	}
	if err := appendJSON(&in, mixed); err != nil {
		return nil, err
	}

	if opts.EnableTUN {
		tun := tunInbound{
			Type: "tun",
			Tag:  "tun-in",
			// Только IPv4: IPv6 в туннеле отключаем, т.к. большинство нод не имеют
			// IPv6-выхода и трафик к dual-stack сайтам (Google) уходит в никуда.
			Address:                []string{"172.19.0.1/30"},
			AutoRoute:              true,
			StrictRoute:            true,
			Stack:                  opts.TUNStack,
			MTU:                    9000,
			EndpointIndependentNAT: true,
		}
		if err := appendJSON(&in, tun); err != nil {
			return nil, err
		}
	}
	return in, nil
}

// appendJSON маршалит значения и добавляет их в срез RawMessage.
func appendJSON(dst *[]json.RawMessage, values ...interface{}) error {
	for _, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		*dst = append(*dst, raw)
	}
	return nil
}
