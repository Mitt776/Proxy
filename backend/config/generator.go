package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Теги ключевых outbound-ов, на которые ссылаются route.final и Clash API.
const (
	ProxyTag  = "proxy"  // selector — то, что выбирает пользователь в UI
	AutoTag   = "auto"   // urltest — автоподбор лучшей ноды по задержке
	DirectTag = "direct" // прямое соединение
)

// Defaults подставляет разумные значения в незаданные поля Options.
func (o *Options) Defaults() {
	if o.MixedPort == 0 {
		o.MixedPort = 2080
	}
	if o.ClashAPIPort == 0 {
		o.ClashAPIPort = 9090
	}
	if o.LogLevel == "" {
		o.LogLevel = "info"
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

	outbounds, finalTag, err := buildOutbounds(nodeTags, nodeOutbounds)
	if err != nil {
		return nil, err
	}

	inbounds, err := buildInbounds(opts)
	if err != nil {
		return nil, err
	}

	hasProxy := finalTag == ProxyTag
	routeRules, ruleSets, err := buildRoute(opts)
	if err != nil {
		return nil, err
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

// Режимы маршрутизации.
const (
	RoutingGlobal   = "global"    // весь трафик через прокси
	RoutingRUDirect = "ru-direct" // РФ-домены/IP и приватные сети — напрямую
)

// Теги/имена файлов локальных rule-set'ов (лежат в каталоге ассетов).
const (
	rsGeoIPRU    = "geoip-ru"
	rsGeositeRU  = "geosite-ru"
	rsGeositeAds = "geosite-ads"
)

// buildRoute собирает правила маршрутизации и список локальных rule-set'ов.
// Базово: sniff (определение протокола) + hijack-dns (перехват DNS в TUN).
// Приватные адреса (LAN/роутер) всегда идут напрямую. Опционально: блок рекламы
// и сплит-туннель «РФ напрямую».
func buildRoute(opts Options) (rules []json.RawMessage, sets []ruleSet, err error) {
	if err = appendJSON(&rules,
		map[string]interface{}{"action": "sniff"},
		map[string]interface{}{"protocol": "dns", "action": "hijack-dns"},
	); err != nil {
		return nil, nil, err
	}

	// Режем QUIC в TUN: на TCP-нодах UDP:443 не проходит, и браузер зависает на
	// HTTP-3 вместо fallback на TCP (ломаются Google/YouTube/медиа). reject
	// заставляет клиента сразу перейти на HTTP/2 поверх TCP.
	if opts.EnableTUN && opts.BlockQUIC {
		if err = appendJSON(&rules, map[string]interface{}{
			"protocol": "quic", "action": "reject",
		}); err != nil {
			return nil, nil, err
		}
	}

	add := func(tag, file string) {
		sets = append(sets, ruleSet{
			Type:   "local",
			Tag:    tag,
			Format: "binary",
			Path:   filepath.Join(opts.RuleSetDir, file),
		})
	}

	// Свои правила (высший приоритет): блок → напрямую → через прокси.
	hasProxy := len(opts.Nodes) > 0
	if err = appendDomainRule(&rules, opts.BlockDomains, "reject", ""); err != nil {
		return nil, nil, err
	}
	if err = appendDomainRule(&rules, opts.DirectDomains, "", DirectTag); err != nil {
		return nil, nil, err
	}
	if hasProxy { // proxy-outbound существует только когда есть ноды
		if err = appendDomainRule(&rules, opts.ProxyDomains, "", ProxyTag); err != nil {
			return nil, nil, err
		}
	}

	// Блокировка рекламных доменов.
	if opts.BlockAds && opts.RuleSetDir != "" {
		add(rsGeositeAds, rsGeositeAds+".srs")
		if err = appendJSON(&rules, map[string]interface{}{
			"rule_set": []string{rsGeositeAds}, "action": "reject",
		}); err != nil {
			return nil, nil, err
		}
	}

	// Приватные адреса — всегда напрямую (иначе теряется доступ к LAN/роутеру).
	if err = appendJSON(&rules, map[string]interface{}{
		"ip_is_private": true, "outbound": DirectTag,
	}); err != nil {
		return nil, nil, err
	}

	// Сплит-туннель: РФ-домены и IP идут напрямую, остальное — через прокси.
	if opts.RoutingMode == RoutingRUDirect && opts.RuleSetDir != "" {
		add(rsGeoIPRU, rsGeoIPRU+".srs")
		add(rsGeositeRU, rsGeositeRU+".srs")
		if err = appendJSON(&rules, map[string]interface{}{
			"rule_set": []string{rsGeoIPRU, rsGeositeRU}, "outbound": DirectTag,
		}); err != nil {
			return nil, nil, err
		}
	}

	return rules, sets, nil
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

// buildOutbounds формирует итоговый список outbound-ов и тег для route.final.
// Если нод нет — трафик идёт напрямую (final = direct), что позволяет проверить
// запуск ядра без реального сервера.
func buildOutbounds(nodeTags []string, nodeOutbounds []json.RawMessage) ([]json.RawMessage, string, error) {
	var out []json.RawMessage
	final := DirectTag

	if len(nodeTags) > 0 {
		final = ProxyTag

		selector := selectorOutbound{
			Type:      "selector",
			Tag:       ProxyTag,
			Outbounds: append([]string{AutoTag}, nodeTags...),
			Default:   AutoTag,
		}
		urltest := urltestOutbound{
			Type:      "urltest",
			Tag:       AutoTag,
			Outbounds: nodeTags,
			URL:       "https://www.gstatic.com/generate_204",
			Interval:  "3m",
		}
		if err := appendJSON(&out, selector, urltest); err != nil {
			return nil, "", err
		}
		out = append(out, nodeOutbounds...)
	}

	if err := appendJSON(&out, simpleOutbound{Type: "direct", Tag: DirectTag}); err != nil {
		return nil, "", err
	}
	return out, final, nil
}

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

// appendDomainRule добавляет правило по списку доменов (domain_suffix — совпадает
// и с поддоменами). action != "" → это действие (напр. reject); иначе — outbound.
// Пустой/из пробелов список пропускается.
func appendDomainRule(dst *[]json.RawMessage, domains []string, action, outbound string) error {
	var clean []string
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			clean = append(clean, d)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	rule := map[string]interface{}{"domain_suffix": clean}
	if action != "" {
		rule["action"] = action
	} else {
		rule["outbound"] = outbound
	}
	return appendJSON(dst, rule)
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
