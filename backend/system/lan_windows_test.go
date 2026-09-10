package system

import (
	"encoding/json"
	"net"
	"testing"

	"Proxy/backend/config"
	"Proxy/backend/rules"
)

// TestIsLANAddress — что мы готовы предложить пользователю как «адрес прокси для
// телефона», а что нет. Главное здесь — публичный адрес: показать его рядом с
// такой подписью значило бы посоветовать выставить прокси без пароля в интернет.
func TestIsLANAddress(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
		why  string
	}{
		{"192.168.1.5", true, "домашняя сеть"},
		{"192.168.137.1", true, "адрес раздачи Wi-Fi в Windows"},
		{"10.8.0.3", true, "частная сеть"},
		{"172.20.4.7", true, "частная сеть 172.16/12"},
		{"169.254.10.2", true, "APIPA — пока ICS не выдал адрес"},
		{"172.19.0.1", false, "адрес нашего же туннеля"},
		{"93.184.216.34", false, "публичный адрес"},
		{"8.8.8.8", false, "публичный адрес"},
	}
	for _, c := range cases {
		if got := isLANAddress(net.ParseIP(c.ip).To4()); got != c.want {
			t.Errorf("isLANAddress(%s) = %v, ожидалось %v (%s)", c.ip, got, c.want, c.why)
		}
	}
	if isLANAddress(nil) {
		t.Error("nil не адрес")
	}
}

// TestTunSubnetMatchesConfig — подсеть туннеля продублирована в двух пакетах
// (config собирает конфиг, system фильтрует по ней список адресов). Разъедутся —
// и в списке «впишите этот адрес» появится адрес, до которого никто не дозвонится.
func TestTunSubnetMatchesConfig(t *testing.T) {
	raw, err := config.Generate(config.Options{
		EnableTUN: true,
		Routing:   rules.Config{Version: rules.Version, Final: rules.ActionDirect},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var cfg struct {
		Inbounds []struct {
			Type    string   `json:"type"`
			Address []string `json:"address"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("конфиг не разбирается: %v", err)
	}
	found := false
	for _, in := range cfg.Inbounds {
		if in.Type != "tun" {
			continue
		}
		for _, addr := range in.Address {
			ip, _, err := net.ParseCIDR(addr)
			if err != nil {
				t.Fatalf("адрес туннеля %q не разбирается: %v", addr, err)
			}
			if ip4 := ip.To4(); ip4 != nil {
				found = true
				if !tunSubnet.Contains(ip4) {
					t.Errorf("адрес туннеля %s вне %s — константы разъехались", ip4, tunSubnet)
				}
			}
		}
	}
	if !found {
		t.Fatal("в конфиге нет tun-инбаунда с IPv4-адресом")
	}
}
