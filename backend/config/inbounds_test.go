package config

import (
	"encoding/json"
	"testing"

	"Proxy/backend/rules"
)

// inbounds достаёт инбаунды сгенерированного конфига: структура generated их не
// разбирает, а здесь проверяется именно она.
func inbounds(t *testing.T, opts Options) map[string]map[string]any {
	t.Helper()
	raw, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var cfg struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("конфиг не разбирается: %v\n%s", err, raw)
	}
	out := map[string]map[string]any{}
	for _, in := range cfg.Inbounds {
		tag, _ := in["tag"].(string)
		out[tag] = in
	}
	return out
}

func lanOptions() Options {
	return Options{
		Nodes:   testNodes("node-1"),
		Routing: rules.Config{Version: rules.Version, Final: rules.ActionProxy},
	}
}

// TestMixedListensLoopbackByDefault — прокси не должен оказаться открытым наружу
// сам по себе: он без аутентификации, и на чужой сети это релей для соседей.
func TestMixedListensLoopbackByDefault(t *testing.T) {
	in := inbounds(t, lanOptions())
	if got := in["mixed-in"]["listen"]; got != "127.0.0.1" {
		t.Fatalf("listen по умолчанию = %v, ожидался 127.0.0.1", got)
	}
}

// TestMixedListensLANWhenAllowed — с AllowLAN прокси виден остальной сети.
// Ради этого настройка и существует: устройства, подключённые к раздаваемому с
// этой машины Wi-Fi, до 127.0.0.1 не достучатся никак.
func TestMixedListensLANWhenAllowed(t *testing.T) {
	opts := lanOptions()
	opts.AllowLAN = true
	in := inbounds(t, opts)
	if got := in["mixed-in"]["listen"]; got != "0.0.0.0" {
		t.Fatalf("listen при AllowLAN = %v, ожидался 0.0.0.0", got)
	}
	// Порт при этом прежний: адрес прокси на самой машине не меняется, иначе
	// системный прокси Windows указывал бы в пустоту.
	if got := in["mixed-in"]["listen_port"]; got != float64(2080) {
		t.Fatalf("порт при AllowLAN = %v, ожидался 2080", got)
	}
}

// TestAllowLANDoesNotTouchTUN — AllowLAN про локальный прокси, туннеля он не
// касается: перепутать эти две вещи легко, а последствия разные.
func TestAllowLANDoesNotTouchTUN(t *testing.T) {
	opts := lanOptions()
	opts.AllowLAN = true
	opts.EnableTUN = true
	tun := inbounds(t, opts)["tun-in"]
	if tun == nil {
		t.Fatal("tun-инбаунд не сгенерирован")
	}
	if got := tun["strict_route"]; got != true {
		t.Fatalf("strict_route = %v, ожидался true", got)
	}
}

// TestStrictRouteDefaultsOn — строгая маршрутизация остаётся дефолтом: это
// защита от утечек DNS и IPv6 мимо туннеля, и нулевое значение настройки обязано
// означать именно её.
func TestStrictRouteDefaultsOn(t *testing.T) {
	opts := lanOptions()
	opts.EnableTUN = true
	if got := inbounds(t, opts)["tun-in"]["strict_route"]; got != true {
		t.Fatalf("strict_route по умолчанию = %v, ожидался true", got)
	}
}

// TestStrictRouteDisabled — тумблер для случаев, когда фильтры WFP мешают
// виртуальным адаптерам (раздача Wi-Fi через ICS, Hyper-V, VMware).
func TestStrictRouteDisabled(t *testing.T) {
	opts := lanOptions()
	opts.EnableTUN = true
	opts.NoStrictRoute = true
	tun := inbounds(t, opts)["tun-in"]
	if got := tun["strict_route"]; got != false {
		t.Fatalf("strict_route = %v, ожидался false", got)
	}
	// auto_route снимать нельзя ни при каких обстоятельствах: без него в туннель
	// не уйдёт вообще ничего, и «совместимость» превратилась бы в мёртвый TUN.
	if got := tun["auto_route"]; got != true {
		t.Fatalf("auto_route = %v, ожидался true", got)
	}
}
