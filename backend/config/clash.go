package config

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// parseClashYAML извлекает ноды из Clash-конфига (секция proxies),
// конвертируя основные типы в outbound-ы sing-box.
func parseClashYAML(data []byte) ([]Node, error) {
	var doc struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("clash yaml: %w", err)
	}
	var nodes []Node
	for _, p := range doc.Proxies {
		n, err := clashToNode(p)
		if err != nil {
			continue // пропускаем неподдерживаемые прокси
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("в clash-конфиге нет поддерживаемых прокси")
	}
	return nodes, nil
}

func clashToNode(p map[string]interface{}) (Node, error) {
	typ := cs(p, "type")
	tag := cs(p, "name")
	server := cs(p, "server")
	port := ci(p, "port")
	if server == "" || port == 0 {
		return Node{}, fmt.Errorf("нет server/port")
	}
	if tag == "" {
		tag = server
	}

	ob := map[string]interface{}{"tag": tag, "server": server, "server_port": port}

	switch typ {
	case "vless":
		ob["type"] = "vless"
		ob["uuid"] = cs(p, "uuid")
		if flow := cs(p, "flow"); flow != "" {
			ob["flow"] = flow
		}
		if tls := clashTLS(p); tls != nil {
			ob["tls"] = tls
		}
		tr, err := clashTransport(p)
		if err != nil {
			return Node{}, fmt.Errorf("vless %q: %w", tag, err)
		}
		if tr != nil {
			ob["transport"] = tr
		}
	case "vmess":
		ob["type"] = "vmess"
		ob["uuid"] = cs(p, "uuid")
		ob["alter_id"] = ci(p, "alterId")
		if cipher := cs(p, "cipher"); cipher != "" {
			ob["security"] = cipher
		} else {
			ob["security"] = "auto"
		}
		if cb(p, "tls") {
			ob["tls"] = clashTLS(p)
		}
		tr, err := clashTransport(p)
		if err != nil {
			return Node{}, fmt.Errorf("vmess %q: %w", tag, err)
		}
		if tr != nil {
			ob["transport"] = tr
		}
	case "trojan":
		ob["type"] = "trojan"
		ob["password"] = cs(p, "password")
		ob["tls"] = clashTLS(p)
		tr, err := clashTransport(p)
		if err != nil {
			return Node{}, fmt.Errorf("trojan %q: %w", tag, err)
		}
		if tr != nil {
			ob["transport"] = tr
		}
	case "ss", "shadowsocks":
		ob["type"] = "shadowsocks"
		ob["method"] = cs(p, "cipher")
		ob["password"] = cs(p, "password")
	case "hysteria2", "hy2":
		ob["type"] = "hysteria2"
		ob["password"] = cs(p, "password")
		ob["tls"] = clashTLS(p)
		if obfs := cs(p, "obfs"); obfs != "" {
			o := map[string]interface{}{"type": obfs}
			if pw := cs(p, "obfs-password"); pw != "" {
				o["password"] = pw
			}
			ob["obfs"] = o
		}
	case "tuic":
		ob["type"] = "tuic"
		ob["uuid"] = cs(p, "uuid")
		ob["password"] = cs(p, "password")
		ob["tls"] = clashTLS(p)
		if cc := cs(p, "congestion-controller"); cc != "" {
			ob["congestion_control"] = cc
		}
		if urm := cs(p, "udp-relay-mode"); urm != "" {
			ob["udp_relay_mode"] = urm
		}
	case "anytls":
		ob["type"] = "anytls"
		ob["password"] = cs(p, "password")
		ob["tls"] = clashTLS(p)
	default:
		return Node{}, fmt.Errorf("тип %q не поддерживается", typ)
	}

	return toNode(tag, ob)
}

// clashTLS собирает секцию tls из полей Clash-прокси.
func clashTLS(p map[string]interface{}) map[string]interface{} {
	tls := map[string]interface{}{"enabled": true}
	if sni := firstNonEmpty(cs(p, "servername"), cs(p, "sni"), cs(p, "server")); sni != "" {
		tls["server_name"] = sni
	}
	if cb(p, "skip-cert-verify") {
		tls["insecure"] = true
	}
	if fp := cs(p, "client-fingerprint"); fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}
	if alpn, ok := p["alpn"].([]interface{}); ok && len(alpn) > 0 {
		var list []string
		for _, a := range alpn {
			list = append(list, fmt.Sprint(a))
		}
		tls["alpn"] = list
	}
	if ro, ok := p["reality-opts"].(map[string]interface{}); ok {
		reality := map[string]interface{}{"enabled": true}
		if pbk := cs(ro, "public-key"); pbk != "" {
			reality["public_key"] = pbk
		}
		if sid := cs(ro, "short-id"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
		if _, hasFP := tls["utls"]; !hasFP {
			tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": "chrome"}
		}
	}
	return tls
}

// clashTransport конвертирует network + *-opts в transport sing-box.
// Для tcp/пусто — (nil, nil); для неподдерживаемого ядром (xhttp и пр.) — ошибка.
func clashTransport(p map[string]interface{}) (map[string]interface{}, error) {
	switch cs(p, "network") {
	case "", "tcp", "raw", "none", "http/2", "h2c":
		return nil, nil // raw TCP — секция transport не нужна
	case "ws":
		tr := map[string]interface{}{"type": "ws"}
		if wo, ok := p["ws-opts"].(map[string]interface{}); ok {
			if path := cs(wo, "path"); path != "" {
				tr["path"] = path
			}
			if hdr, ok := wo["headers"].(map[string]interface{}); ok {
				if host := cs(hdr, "Host"); host != "" {
					tr["headers"] = map[string]interface{}{"Host": host}
				}
			}
		}
		return tr, nil
	case "grpc":
		tr := map[string]interface{}{"type": "grpc"}
		if go_, ok := p["grpc-opts"].(map[string]interface{}); ok {
			if sn := cs(go_, "grpc-service-name"); sn != "" {
				tr["service_name"] = sn
			}
		}
		return tr, nil
	case "http", "h2":
		tr := map[string]interface{}{"type": "http"}
		if ho, ok := p["http-opts"].(map[string]interface{}); ok {
			if path := cs(ho, "path"); path != "" {
				tr["path"] = path
			}
		}
		return tr, nil
	case "httpupgrade":
		tr := map[string]interface{}{"type": "httpupgrade"}
		if ho, ok := p["ws-opts"].(map[string]interface{}); ok {
			if path := cs(ho, "path"); path != "" {
				tr["path"] = path
			}
		}
		return tr, nil
	case "xhttp", "splithttp":
		return nil, fmt.Errorf("транспорт xhttp не поддерживается sing-box (это транспорт Xray-core)")
	default:
		return nil, fmt.Errorf("неизвестный транспорт %q (sing-box умеет: tcp, ws, grpc, http, httpupgrade)", cs(p, "network"))
	}
}

// --- helpers для map[string]interface{} из YAML ---

func cs(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func ci(m map[string]interface{}, key string) int {
	switch t := m[key].(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func cb(m map[string]interface{}, key string) bool {
	switch t := m[key].(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}
