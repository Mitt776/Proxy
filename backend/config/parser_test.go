package config

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
)

func vmessSample() string {
	j := `{"v":"2","ps":"vm-node","add":"vm.example.com","port":"443","id":"11111111-1111-1111-1111-111111111111","aid":"0","net":"ws","host":"vm.example.com","path":"/ray","tls":"tls","scy":"auto"}`
	return "vmess://" + base64.StdEncoding.EncodeToString([]byte(j))
}

func TestParseLinksAllProtocols(t *testing.T) {
	cases := []struct {
		name     string
		link     string
		wantType string
		wantTag  string
	}{
		{
			name:     "vless-reality-vision",
			link:     "vless://11111111-1111-1111-1111-111111111111@vless.example.com:443?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&security=reality&sid=9ede&sni=yahoo.com&type=tcp#reality-node",
			wantType: "vless",
			wantTag:  "reality-node",
		},
		{name: "vmess-ws-tls", link: vmessSample(), wantType: "vmess", wantTag: "vm-node"},
		{
			name:     "trojan",
			link:     "trojan://pass123@tr.example.com:443?sni=tr.example.com&type=tcp#trojan-node",
			wantType: "trojan", wantTag: "trojan-node",
		},
		{
			name:     "ss-sip002",
			link:     "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@ss.example.com:8388#ss-node",
			wantType: "shadowsocks", wantTag: "ss-node",
		},
		{
			name:     "hysteria2",
			link:     "hysteria2://pw@hy.example.com:8443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=xyz#hy2-node",
			wantType: "hysteria2", wantTag: "hy2-node",
		},
		{
			name:     "tuic",
			link:     "tuic://11111111-1111-1111-1111-111111111111:pw@tuic.example.com:443?sni=tuic.example.com&congestion_control=bbr&udp_relay_mode=native#tuic-node",
			wantType: "tuic", wantTag: "tuic-node",
		},
		{
			name:     "anytls",
			link:     "anytls://pw@at.example.com:443?sni=at.example.com&insecure=1#anytls-node",
			wantType: "anytls", wantTag: "anytls-node",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node, err := ParseLink(c.link)
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			if node.Tag != c.wantTag {
				t.Errorf("tag = %q, want %q", node.Tag, c.wantTag)
			}
			var ob map[string]interface{}
			if err := json.Unmarshal(node.Outbound, &ob); err != nil {
				t.Fatalf("outbound не JSON: %v", err)
			}
			if ob["type"] != c.wantType {
				t.Errorf("type = %v, want %q", ob["type"], c.wantType)
			}
			if ob["server"] == "" || ob["server_port"] == nil {
				t.Errorf("нет server/server_port: %v", ob)
			}
		})
	}
}

// TestUnsupportedTransportRejected проверяет, что действительно неизвестные типы
// транспорта дают понятную ошибку, а не молча трактуются как TCP.
func TestUnsupportedTransportRejected(t *testing.T) {
	bad := []string{
		"vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com&type=kcp#n",
		"trojan://pw@x.com:443?sni=x.com&type=quic#n",
	}
	for _, link := range bad {
		if _, err := ParseLink(link); err == nil {
			t.Errorf("ожидалась ошибка для %.40s…, но её нет", link)
		}
	}
}

// TestXHTTPParsing проверяет разбор XHTTP-транспорта (для форков sing-box с
// with_xhttp): базовые поля host/path/mode, срез хвоста в path, отсутствие flow
// (vision несовместим) и слияние параметра extra (camelCase → snake_case).
func TestXHTTPParsing(t *testing.T) {
	link := "vless://11111111-1111-1111-1111-111111111111@x.com:443" +
		"?security=reality&pbk=k&sni=x.com&flow=xtls-rprx-vision" +
		"&type=xhttp&mode=packet-up&host=cdn.x.com&path=" + url.QueryEscape("/down?ed=2048") +
		"&extra=" + url.QueryEscape(`{"xPaddingBytes":"100-1000","noGRPCHeader":true}`) +
		"#xh"
	node, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	var ob struct {
		Flow      string `json:"flow"`
		Transport struct {
			Type         string `json:"type"`
			Mode         string `json:"mode"`
			Host         string `json:"host"`
			Path         string `json:"path"`
			XPaddingByte string `json:"x_padding_bytes"`
			NoGRPCHeader bool   `json:"no_grpc_header"`
		} `json:"transport"`
	}
	if err := json.Unmarshal(node.Outbound, &ob); err != nil {
		t.Fatalf("outbound не JSON: %v", err)
	}
	tr := ob.Transport
	if tr.Type != "xhttp" {
		t.Errorf("type = %q, want xhttp", tr.Type)
	}
	if tr.Mode != "packet-up" || tr.Host != "cdn.x.com" {
		t.Errorf("mode/host неверны: %+v", tr)
	}
	if tr.Path != "/down" {
		t.Errorf("path = %q, want /down (хвост ?… должен срезаться)", tr.Path)
	}
	if ob.Flow != "" {
		t.Errorf("flow = %q, для XHTTP должен быть пустым", ob.Flow)
	}
	if tr.XPaddingByte != "100-1000" || !tr.NoGRPCHeader {
		t.Errorf("поля из extra не влились: x_padding_bytes=%q no_grpc_header=%v", tr.XPaddingByte, tr.NoGRPCHeader)
	}
}

// TestSupportedTransportsParse проверяет, что известные транспорты по-прежнему
// разбираются и дают корректный type в секции transport.
func TestSupportedTransportsParse(t *testing.T) {
	cases := map[string]string{
		"ws":          "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com&type=ws&path=/w#n",
		"grpc":        "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com&type=grpc&serviceName=s#n",
		"http":        "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com&type=http&path=/h#n",
		"httpupgrade": "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com&type=httpupgrade&path=/u#n",
	}
	for want, link := range cases {
		node, err := ParseLink(link)
		if err != nil {
			t.Errorf("%s: ParseLink: %v", want, err)
			continue
		}
		var ob struct {
			Transport struct {
				Type string `json:"type"`
			} `json:"transport"`
		}
		_ = json.Unmarshal(node.Outbound, &ob)
		if ob.Transport.Type != want {
			t.Errorf("%s: transport.type = %q, want %q", want, ob.Transport.Type, want)
		}
	}

	// raw TCP не должен добавлять секцию transport.
	node, err := ParseLink("vless://11111111-1111-1111-1111-111111111111@x.com:443?security=reality&pbk=k&sni=x.com&type=tcp#n")
	if err != nil {
		t.Fatalf("tcp: %v", err)
	}
	var ob map[string]interface{}
	_ = json.Unmarshal(node.Outbound, &ob)
	if _, has := ob["transport"]; has {
		t.Errorf("для type=tcp секция transport не нужна, но она есть")
	}
}

func TestDecodeSubscriptionBase64(t *testing.T) {
	list := "trojan://p@a.com:443#a\nvless://11111111-1111-1111-1111-111111111111@b.com:443?security=reality&pbk=x&sni=c.com#b"
	sub := base64.StdEncoding.EncodeToString([]byte(list))
	nodes, err := DecodeSubscription([]byte(sub))
	if err != nil {
		t.Fatalf("DecodeSubscription: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("нод = %d, want 2", len(nodes))
	}
}

func TestDecodeSubscriptionClash(t *testing.T) {
	yaml := `proxies:
  - name: cl-trojan
    type: trojan
    server: cl.example.com
    port: 443
    password: secret
    sni: cl.example.com
  - name: cl-ss
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: pw
`
	nodes, err := DecodeSubscription([]byte(yaml))
	if err != nil {
		t.Fatalf("DecodeSubscription clash: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("нод = %d, want 2", len(nodes))
	}
}
