package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseLink разбирает одну прокси-ссылку (vless://, vmess://, trojan://, ss://,
// hysteria2://, tuic://, anytls://) в ноду sing-box. Схема определяется по префиксу.
func ParseLink(link string) (Node, error) {
	link = strings.TrimSpace(link)
	scheme := strings.ToLower(schemeOf(link))

	switch scheme {
	case "vless":
		return parseVLESS(link)
	case "vmess":
		return parseVMess(link)
	case "trojan":
		return parseTrojan(link)
	case "ss":
		return parseShadowsocks(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	case "tuic":
		return parseTUIC(link)
	case "anytls":
		return parseAnyTLS(link)
	default:
		return Node{}, fmt.Errorf("неподдерживаемая или неизвестная схема ссылки: %q", scheme)
	}
}

// ParseLinks разбирает много ссылок (по одной на строку), пропуская пустые
// строки и комментарии. Возвращает распарсенные ноды и ошибки по каждой плохой строке.
func ParseLinks(text string) ([]Node, []error) {
	var nodes []Node
	var errs []error
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := ParseLink(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("%.40s…: %w", line, err))
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, errs
}

func schemeOf(link string) string {
	if i := strings.Index(link, "://"); i > 0 {
		return link[:i]
	}
	return ""
}

// --- VLESS (в т.ч. Reality / XTLS-Vision / ws / grpc) ---

func parseVLESS(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, fmt.Errorf("некорректный vless URL: %w", err)
	}
	uuid := u.User.Username()
	if uuid == "" {
		return Node{}, fmt.Errorf("vless: отсутствует UUID")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, fmt.Errorf("vless: %w", err)
	}
	q := u.Query()
	tag := fragTag(u, host)

	ob := map[string]interface{}{
		"type":        "vless",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"uuid":        uuid,
	}
	// XHTTP несовместим с xtls-rprx-vision — flow для него не выставляем.
	if flow := q.Get("flow"); flow != "" && !isXHTTP(q.Get("type")) {
		ob["flow"] = flow
	}
	if pe := q.Get("packetEncoding"); pe != "" {
		ob["packet_encoding"] = pe
	}
	if tls := buildTLS(q); tls != nil {
		ob["tls"] = tls
	}
	tr, err := buildTransport(q)
	if err != nil {
		return Node{}, fmt.Errorf("vless: %w", err)
	}
	if tr != nil {
		ob["transport"] = tr
	}
	return toNode(tag, ob)
}

// buildTLS собирает секцию tls из query-параметров ссылки (security=tls|reality).
// Возвращает nil, если TLS не требуется.
func buildTLS(q url.Values) map[string]interface{} {
	security := q.Get("security")
	if security == "" || security == "none" {
		return nil
	}

	sni := firstNonEmpty(q.Get("sni"), q.Get("peer"), q.Get("host"))
	tls := map[string]interface{}{"enabled": true}
	if sni != "" {
		tls["server_name"] = sni
	}
	if q.Get("allowInsecure") == "1" || strings.EqualFold(q.Get("allowInsecure"), "true") {
		tls["insecure"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = splitCSV(alpn)
	}

	// uTLS-отпечаток (fp). Для Reality обязателен — подставляем chrome по умолчанию.
	fp := q.Get("fp")
	if security == "reality" && fp == "" {
		fp = "chrome"
	}
	if fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}

	if security == "reality" {
		reality := map[string]interface{}{"enabled": true}
		if pbk := q.Get("pbk"); pbk != "" {
			reality["public_key"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
	}
	return tls
}

// buildTransport собирает секцию transport (ws/grpc/http/httpupgrade).
// Для type=tcp/raw (или пусто) транспорт не нужен — возвращает (nil, nil).
// Для транспорта, которого нет в sing-box (напр. xhttp/splithttp из Xray) —
// возвращает понятную ошибку, чтобы нода не добавлялась «молча как TCP».
func buildTransport(q url.Values) (map[string]interface{}, error) {
	switch q.Get("type") {
	case "", "tcp", "raw", "none":
		return nil, nil // raw TCP (в т.ч. Reality/Vision) — секция transport не нужна
	case "ws":
		tr := map[string]interface{}{"type": "ws"}
		if path := q.Get("path"); path != "" {
			tr["path"] = path
		}
		if host := q.Get("host"); host != "" {
			tr["headers"] = map[string]interface{}{"Host": host}
		}
		return tr, nil
	case "grpc":
		tr := map[string]interface{}{"type": "grpc"}
		if sn := firstNonEmpty(q.Get("serviceName"), q.Get("path")); sn != "" {
			tr["service_name"] = sn
		}
		return tr, nil
	case "http", "h2":
		tr := map[string]interface{}{"type": "http"}
		if path := q.Get("path"); path != "" {
			tr["path"] = path
		}
		if host := q.Get("host"); host != "" {
			tr["host"] = []string{host}
		}
		return tr, nil
	case "httpupgrade":
		tr := map[string]interface{}{"type": "httpupgrade"}
		if path := q.Get("path"); path != "" {
			tr["path"] = path
		}
		if host := q.Get("host"); host != "" {
			tr["host"] = host
		}
		return tr, nil
	case "xhttp", "splithttp":
		// XHTTP есть только в форках sing-box (напр. Leadaxe/sing-box-lx, тег
		// with_xhttp). Штатный sing-box такой транспорт отклонит при старте ядра.
		return buildXHTTP(q), nil
	default:
		return nil, fmt.Errorf("неизвестный транспорт %q (sing-box умеет: tcp, ws, grpc, http, httpupgrade)", q.Get("type"))
	}
}

// xhttpFields — соответствие camelCase-параметров ссылки snake_case-полям
// transport'а sing-box-lx (SPEC 002 v2). Явная таблица нужна из-за аббревиатур
// (noGRPCHeader → no_grpc_header), которые ломает наивная конвертация.
var xhttpFields = map[string]string{
	"host": "host", "path": "path", "mode": "mode",
	"xPaddingBytes": "x_padding_bytes", "noGRPCHeader": "no_grpc_header",
	"sessionPlacement": "session_placement", "sessionKey": "session_key",
	"seqPlacement": "seq_placement", "seqKey": "seq_key",
	"uplinkDataPlacement": "uplink_data_placement", "uplinkDataKey": "uplink_data_key",
	"uplinkChunkSize": "uplink_chunk_size", "uplinkHTTPMethod": "uplink_http_method",
	"xPaddingObfsMode": "x_padding_obfs_mode", "xPaddingKey": "x_padding_key",
	"xPaddingHeader": "x_padding_header", "xPaddingPlacement": "x_padding_placement",
	"xPaddingMethod":       "x_padding_method",
	"scMaxEachPostBytes":   "sc_max_each_post_bytes",
	"scMinPostsIntervalMs": "sc_min_posts_interval_ms",
}

// xhttpBoolFields — какие поля привести к bool (в URL/extra приходят строкой).
var xhttpBoolFields = map[string]bool{"no_grpc_header": true, "x_padding_obfs_mode": true}

// buildXHTTP собирает секцию transport типа xhttp из плоских query-параметров и
// (опционально) URL-encoded JSON в параметре extra. Поля, которых нет в ссылке,
// не выставляем — у транспорта корректные дефолты.
func buildXHTTP(q url.Values) map[string]interface{} {
	tr := map[string]interface{}{"type": "xhttp"}

	set := func(camel, raw string) {
		snake, ok := xhttpFields[camel]
		if !ok || raw == "" {
			return
		}
		if snake == "path" {
			raw = stripQuery(raw) // из path убираем хвост "?..."
		}
		if xhttpBoolFields[snake] {
			tr[snake] = raw == "true" || raw == "1"
			return
		}
		tr[snake] = raw
	}

	// Плоские параметры ссылки.
	for camel := range xhttpFields {
		if v := q.Get(camel); v != "" {
			set(camel, v)
		}
	}
	// extra = URL-encoded JSON с дополнительными полями (имеет приоритет).
	if extra := q.Get("extra"); extra != "" {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(extra), &m); err == nil {
			for camel, v := range m {
				set(camel, fmt.Sprint(v))
			}
		}
	}
	return tr
}

// isXHTTP сообщает, относится ли значение type/net к транспорту XHTTP.
func isXHTTP(t string) bool { return t == "xhttp" || t == "splithttp" }

// stripQuery отрезает хвост "?..." (для XHTTP path).
func stripQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
}

// (остальные протоколы — в parser_protocols.go)

// --- общие helpers ---

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("отсутствует хост")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return "", 0, fmt.Errorf("некорректный порт %q", u.Port())
	}
	return host, port, nil
}

// fragTag берёт имя ноды из #фрагмента, иначе — host:port как запасной вариант.
func fragTag(u *url.URL, fallback string) string {
	if u.Fragment != "" {
		if dec, err := url.QueryUnescape(u.Fragment); err == nil {
			return strings.TrimSpace(dec)
		}
		return strings.TrimSpace(u.Fragment)
	}
	return fallback
}

func toNode(tag string, ob map[string]interface{}) (Node, error) {
	raw, err := json.Marshal(ob)
	if err != nil {
		return Node{}, err
	}
	return Node{Tag: tag, Outbound: raw}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
