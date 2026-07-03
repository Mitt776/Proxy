// Package config собирает конфигурацию sing-box из профилей и опций приложения.
package config

import "encoding/json"

// Node — одна распарсенная нода (outbound sing-box) с её тегом.
// Само тело хранится как RawMessage, потому что у разных протоколов
// (vless, hysteria2, wireguard, ...) разный набор полей.
type Node struct {
	Tag      string          `json:"tag"`
	Outbound json.RawMessage `json:"outbound"`
}

// Options — входные параметры генерации config.json.
type Options struct {
	MixedPort    int    // локальный mixed (HTTP+SOCKS) inbound
	ClashAPIPort int    // порт Clash API (external_controller)
	ClashSecret  string // секрет Clash API
	LogLevel     string // trace|debug|info|warn|error

	EnableTUN bool   // добавить tun inbound (полный перехват)
	TUNStack  string // gvisor|system|mixed

	Nodes []Node // распарсенные ноды профиля

	RoutingMode string // "global" (всё через прокси) | "ru-direct" (РФ напрямую)
	BlockAds    bool   // блокировать рекламные домены (geosite-ads)
	BlockQUIC   bool   // резать QUIC/HTTP-3 в TUN (fallback на TCP; чинит Google/YouTube)
	RuleSetDir  string // каталог с .srs (обычно каталог ассетов)

	// Свои правила маршрутизации (домены) поверх пресетов — высший приоритет.
	DirectDomains []string // всегда напрямую
	ProxyDomains  []string // всегда через прокси
	BlockDomains  []string // блокировать

	GeoIPPath   string // путь к geoip.db (для будущих правил)
	GeoSitePath string // путь к geosite.db
	CacheDBPath string // путь к cache.db (clash_api селекторы, кэш)
}

// singBoxConfig — верхнеуровневая схема config.json (только то, что мы генерируем).
type singBoxConfig struct {
	Log          logOptions        `json:"log"`
	DNS          dnsOptions        `json:"dns"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Route        routeOptions      `json:"route"`
	Experimental experimental      `json:"experimental"`
}

// dnsOptions — DNS-резолвер sing-box. Критично для TUN: без него DNS-запросы
// уходят сырым UDP в outbound, а он (напр. VLESS+Vision) может не пропускать UDP.
type dnsOptions struct {
	Servers  []dnsServer `json:"servers"`
	Final    string      `json:"final"`
	Strategy string      `json:"strategy,omitempty"`
}

type dnsServer struct {
	Tag     string `json:"tag"`
	Type    string `json:"type"`             // https|udp|tls|...
	Server  string `json:"server"`           // адрес DNS-сервера
	Detour  string `json:"detour,omitempty"` // через какой outbound слать запросы
}

type logOptions struct {
	Level     string `json:"level"`
	Timestamp bool   `json:"timestamp"`
	Output    string `json:"output,omitempty"` // файл лога (относительно рабочего каталога)
}

type routeOptions struct {
	RuleSet               []ruleSet         `json:"rule_set,omitempty"`
	Rules                 []json.RawMessage `json:"rules,omitempty"`
	Final                 string            `json:"final"`
	AutoDetectInterface   bool              `json:"auto_detect_interface"`
	DefaultDomainResolver *domainResolver   `json:"default_domain_resolver,omitempty"`
}

// ruleSet — локальный бинарный rule-set (.srs) для гео-маршрутизации.
type ruleSet struct {
	Type   string `json:"type"`   // local
	Tag    string `json:"tag"`    //
	Format string `json:"format"` // binary
	Path   string `json:"path"`   // абсолютный путь к .srs
}

// domainResolver указывает, какой DNS резолвит домены серверов outbound
// (напр. домен самого прокси-сервера) — резолвим напрямую, чтобы не было петли.
type domainResolver struct {
	Server string `json:"server"`
}

type experimental struct {
	ClashAPI  clashAPIOptions `json:"clash_api"`
	CacheFile cacheFile       `json:"cache_file"`
}

type clashAPIOptions struct {
	ExternalController string `json:"external_controller"`
	Secret             string `json:"secret,omitempty"`
}

type cacheFile struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
}

// --- структуры для генерируемых inbounds/outbounds ---

type mixedInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type tunInbound struct {
	Type                   string   `json:"type"`
	Tag                    string   `json:"tag"`
	Address                []string `json:"address"`
	AutoRoute              bool     `json:"auto_route"`
	StrictRoute            bool     `json:"strict_route"`
	AutoRedirect           bool     `json:"auto_redirect,omitempty"`
	Stack                  string   `json:"stack"`
	MTU                    int      `json:"mtu,omitempty"`
	EndpointIndependentNAT bool     `json:"endpoint_independent_nat,omitempty"`
}

type selectorOutbound struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	Default   string   `json:"default,omitempty"`
}

type urltestOutbound struct {
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	URL       string   `json:"url,omitempty"`
	Interval  string   `json:"interval,omitempty"`
}

type simpleOutbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}
