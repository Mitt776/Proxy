//go:build coretest

package config

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedConfigValidates генерирует config со всеми протоколами и проверяет
// его настоящим `sing-box check` — гарантия, что парсеры дают валидную схему 1.13.
func TestGeneratedConfigValidates(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан")
	}
	bin := filepath.Join(assets, "sing-box.exe")

	links := []string{
		"vless://11111111-1111-1111-1111-111111111111@vless.example.com:443?flow=xtls-rprx-vision&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&security=reality&sid=9ede&sni=yahoo.com&type=tcp#vless-reality",
		"vless://11111111-1111-1111-1111-111111111111@ws.example.com:443?security=tls&sni=ws.example.com&type=ws&path=/ray&host=ws.example.com#vless-ws",
		vmessSample(),
		"trojan://pass123@tr.example.com:443?sni=tr.example.com#trojan",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@ss.example.com:8388#ss",
		"hysteria2://pw@hy.example.com:8443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=xyz#hy2",
		"tuic://11111111-1111-1111-1111-111111111111:pw@tuic.example.com:443?sni=tuic.example.com&congestion_control=bbr&udp_relay_mode=native#tuic",
		"anytls://pw@at.example.com:443?sni=at.example.com&insecure=1#anytls",
	}

	var nodes []Node
	for _, l := range links {
		n, err := ParseLink(l)
		if err != nil {
			t.Fatalf("ParseLink(%.30s): %v", l, err)
		}
		nodes = append(nodes, n)
	}

	cfg, err := Generate(Options{
		ClashSecret: "x", CacheDBPath: "cache.db", Nodes: nodes,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(bin, "check", "-c", cfgPath, "-D", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check ПРОВАЛИЛСЯ: %v\n%s\n---config---\n%s", err, out, cfg)
	}
	t.Logf("✅ sing-box check прошёл для %d нод всех протоколов", len(nodes))
}

// TestRoutingConfigValidates проверяет конфиг со сплит-туннелем «РФ напрямую» и
// блокировкой рекламы: валидирует rule_set (.srs), ip_is_private и action reject.
func TestRoutingConfigValidates(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан")
	}
	bin := filepath.Join(assets, "sing-box.exe")

	node, err := ParseLink("trojan://pw@tr.example.com:443?sni=tr.example.com#n")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Generate(Options{
		ClashSecret: "x", CacheDBPath: "cache.db",
		Nodes:         []Node{node},
		RoutingMode:   RoutingRUDirect,
		BlockAds:      true,
		RuleSetDir:    assets,
		DirectDomains: []string{"example.com", "  LOCAL.dev "},
		ProxyDomains:  []string{"youtube.com"},
		BlockDomains:  []string{"ads.example.net"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", cfgPath, "-D", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check маршрутизации ПРОВАЛИЛСЯ: %v\n%s\n---config---\n%s", err, out, cfg)
	}
	t.Log("✅ конфиг сплит-туннеля РФ + блок рекламы валиден")
}

// TestTUNConfigValidates проверяет, что конфиг с включённым TUN валиден по схеме
// (реальный адаптер требует прав администратора и проверяется в GUI).
func TestTUNConfigValidates(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан")
	}
	bin := filepath.Join(assets, "sing-box.exe")

	node, err := ParseLink("trojan://pw@tr.example.com:443?sni=tr.example.com#n")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Generate(Options{
		ClashSecret: "x", CacheDBPath: "cache.db",
		EnableTUN: true, TUNStack: "gvisor",
		BlockQUIC: true,
		Nodes:     []Node{node},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", cfgPath, "-D", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check TUN ПРОВАЛИЛСЯ: %v\n%s\n---config---\n%s", err, out, cfg)
	}
	// Убеждаемся, что правило reject QUIC действительно попало в конфиг.
	if !strings.Contains(string(cfg), "\"quic\"") {
		t.Fatalf("в TUN-конфиге нет правила reject QUIC:\n%s", cfg)
	}
	t.Log("✅ TUN-конфиг валиден, QUIC режется")
}
