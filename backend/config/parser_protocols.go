package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// --- VMess (формат v2rayN: vmess://base64(JSON)) ---

func parseVMess(link string) (Node, error) {
	payload := strings.TrimPrefix(link, "vmess://")
	decoded, err := decodeBase64(payload)
	if err != nil {
		return Node{}, fmt.Errorf("vmess: не удалось декодировать base64: %w", err)
	}
	var v struct {
		PS   string      `json:"ps"`
		Add  string      `json:"add"`
		Port interface{} `json:"port"`
		ID   string      `json:"id"`
		Aid  interface{} `json:"aid"`
		Scy  string      `json:"scy"`
		Net  string      `json:"net"`
		Type string      `json:"type"`
		Host string      `json:"host"`
		Path string      `json:"path"`
		TLS  string      `json:"tls"`
		SNI  string      `json:"sni"`
		ALPN string      `json:"alpn"`
		FP   string      `json:"fp"`
	}
	if err := json.Unmarshal(decoded, &v); err != nil {
		return Node{}, fmt.Errorf("vmess: некорректный JSON: %w", err)
	}
	port := toInt(v.Port)
	if v.Add == "" || port == 0 || v.ID == "" {
		return Node{}, fmt.Errorf("vmess: отсутствует add/port/id")
	}
	tag := firstNonEmpty(v.PS, v.Add)

	security := v.Scy
	if security == "" {
		security = "auto"
	}
	ob := map[string]interface{}{
		"type":        "vmess",
		"tag":         tag,
		"server":      v.Add,
		"server_port": port,
		"uuid":        v.ID,
		"security":    security,
		"alter_id":    toInt(v.Aid),
	}
	if strings.EqualFold(v.TLS, "tls") || strings.EqualFold(v.TLS, "reality") {
		tls := map[string]interface{}{"enabled": true}
		if sni := firstNonEmpty(v.SNI, v.Host); sni != "" {
			tls["server_name"] = sni
		}
		if v.ALPN != "" {
			tls["alpn"] = splitCSV(v.ALPN)
		}
		if v.FP != "" {
			tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": v.FP}
		}
		ob["tls"] = tls
	}
	tr, err := vmessTransport(v.Net, v.Host, v.Path)
	if err != nil {
		return Node{}, fmt.Errorf("vmess: %w", err)
	}
	if tr != nil {
		ob["transport"] = tr
	}
	return toNode(tag, ob)
}

func vmessTransport(net, host, path string) (map[string]interface{}, error) {
	switch net {
	case "", "tcp", "raw", "none":
		return nil, nil // raw TCP — секция transport не нужна
	case "ws":
		tr := map[string]interface{}{"type": "ws"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["headers"] = map[string]interface{}{"Host": host}
		}
		return tr, nil
	case "grpc":
		tr := map[string]interface{}{"type": "grpc"}
		if path != "" {
			tr["service_name"] = path
		}
		return tr, nil
	case "h2", "http":
		tr := map[string]interface{}{"type": "http"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["host"] = []string{host}
		}
		return tr, nil
	case "httpupgrade":
		tr := map[string]interface{}{"type": "httpupgrade"}
		if path != "" {
			tr["path"] = path
		}
		if host != "" {
			tr["host"] = host
		}
		return tr, nil
	case "xhttp", "splithttp":
		return nil, fmt.Errorf("транспорт %q не поддерживается sing-box (это транспорт Xray-core)", net)
	default:
		return nil, fmt.Errorf("неизвестный транспорт %q (sing-box умеет: tcp, ws, grpc, http, httpupgrade)", net)
	}
}

// --- Trojan ---

func parseTrojan(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, fmt.Errorf("trojan: некорректный URL: %w", err)
	}
	password := u.User.Username()
	if password == "" {
		return Node{}, fmt.Errorf("trojan: отсутствует пароль")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, fmt.Errorf("trojan: %w", err)
	}
	q := u.Query()
	tag := fragTag(u, host)

	ob := map[string]interface{}{
		"type":        "trojan",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    password,
	}
	// Trojan работает поверх TLS по умолчанию (кроме security=none).
	if q.Get("security") != "none" {
		ob["tls"] = simpleTLS(q, host)
	}
	tr, err := buildTransport(q)
	if err != nil {
		return Node{}, fmt.Errorf("trojan: %w", err)
	}
	if tr != nil {
		ob["transport"] = tr
	}
	return toNode(tag, ob)
}

// --- Shadowsocks (SIP002 и legacy base64) ---

func parseShadowsocks(link string) (Node, error) {
	rest := strings.TrimPrefix(link, "ss://")

	// Отделяем #tag.
	tag := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		if dec, err := url.QueryUnescape(rest[i+1:]); err == nil {
			tag = strings.TrimSpace(dec)
		}
		rest = rest[:i]
	}
	// Отбрасываем query (?plugin=...) — плагины пока не поддерживаем.
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}

	var method, password, host string
	var port int

	if at := strings.LastIndex(rest, "@"); at >= 0 {
		// SIP002: ss://base64(method:password)@host:port
		userInfo := rest[:at]
		if dec, err := decodeBase64(userInfo); err == nil {
			method, password = splitPair(string(dec))
		} else {
			method, password = splitPair(userInfo) // уже открытым текстом
		}
		host, port = splitHostPort(rest[at+1:])
	} else {
		// legacy: ss://base64(method:password@host:port)
		dec, err := decodeBase64(rest)
		if err != nil {
			return Node{}, fmt.Errorf("ss: не удалось декодировать: %w", err)
		}
		s := string(dec)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return Node{}, fmt.Errorf("ss: неверный формат")
		}
		method, password = splitPair(s[:at])
		host, port = splitHostPort(s[at+1:])
	}

	if host == "" || port == 0 || method == "" {
		return Node{}, fmt.Errorf("ss: отсутствует method/host/port")
	}
	if tag == "" {
		tag = host
	}
	ob := map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}
	return toNode(tag, ob)
}

// --- Hysteria2 ---

func parseHysteria2(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, fmt.Errorf("hysteria2: некорректный URL: %w", err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, fmt.Errorf("hysteria2: %w", err)
	}
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password = password + ":" + pw
	}
	q := u.Query()
	tag := fragTag(u, host)

	ob := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         simpleTLS(q, host),
	}
	if obfs := q.Get("obfs"); obfs != "" {
		o := map[string]interface{}{"type": obfs}
		if p := firstNonEmpty(q.Get("obfs-password"), q.Get("obfs_password")); p != "" {
			o["password"] = p
		}
		ob["obfs"] = o
	}
	return toNode(tag, ob)
}

// --- TUIC v5 ---

func parseTUIC(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, fmt.Errorf("tuic: некорректный URL: %w", err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, fmt.Errorf("tuic: %w", err)
	}
	uuid := u.User.Username()
	password, _ := u.User.Password()
	if uuid == "" {
		return Node{}, fmt.Errorf("tuic: отсутствует UUID")
	}
	q := u.Query()
	tag := fragTag(u, host)

	ob := map[string]interface{}{
		"type":        "tuic",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"uuid":        uuid,
		"password":    password,
		"tls":         simpleTLS(q, host),
	}
	if cc := firstNonEmpty(q.Get("congestion_control"), q.Get("congestion")); cc != "" {
		ob["congestion_control"] = cc
	}
	if urm := firstNonEmpty(q.Get("udp_relay_mode"), q.Get("udp")); urm != "" {
		ob["udp_relay_mode"] = urm
	}
	return toNode(tag, ob)
}

// --- AnyTLS ---

func parseAnyTLS(link string) (Node, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Node{}, fmt.Errorf("anytls: некорректный URL: %w", err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Node{}, fmt.Errorf("anytls: %w", err)
	}
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password = pw // формат anytls://password@host — пароль как username
	}
	q := u.Query()
	tag := fragTag(u, host)

	ob := map[string]interface{}{
		"type":        "anytls",
		"tag":         tag,
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         simpleTLS(q, host),
	}
	return toNode(tag, ob)
}

// --- helpers ---

// simpleTLS собирает секцию tls для протоколов, где TLS всегда включён
// (trojan/hysteria2/tuic/anytls).
func simpleTLS(q url.Values, defaultSNI string) map[string]interface{} {
	tls := map[string]interface{}{"enabled": true}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer"), defaultSNI); sni != "" {
		tls["server_name"] = sni
	}
	if isTrue(q.Get("insecure")) || isTrue(q.Get("allowInsecure")) {
		tls["insecure"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = splitCSV(alpn)
	}
	if fp := q.Get("fp"); fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}
	return tls
}

// decodeBase64 декодирует строку в любом из вариантов base64 (std/url, с/без padding).
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}

func splitPair(s string) (a, b string) {
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func splitHostPort(s string) (string, int) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		port, _ := strconv.Atoi(s[i+1:])
		return s[:i], port
	}
	return s, 0
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case int:
		return t
	default:
		return 0
	}
}

func isTrue(s string) bool {
	return s == "1" || strings.EqualFold(s, "true")
}
