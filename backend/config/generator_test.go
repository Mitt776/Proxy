package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"Proxy/backend/rules"
)

// generated — распакованный конфиг для проверок в тестах.
type generated struct {
	Route struct {
		Rules   []map[string]any `json:"rules"`
		RuleSet []struct {
			Type           string `json:"type"`
			Tag            string `json:"tag"`
			Path           string `json:"path"`
			Format         string `json:"format"`
			URL            string `json:"url"`
			DownloadDetour string `json:"download_detour"`
			UpdateInterval string `json:"update_interval"`
		} `json:"rule_set"`
		Final string `json:"final"`
	} `json:"route"`
	Outbounds []map[string]any `json:"outbounds"`
	DNS       struct {
		Servers []map[string]any `json:"servers"`
		Final   string           `json:"final"`
	} `json:"dns"`
}

func gen(t *testing.T, opts Options) generated {
	t.Helper()
	raw, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var g generated
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("конфиг не разбирается: %v\n%s", err, raw)
	}
	return g
}

// testNodes возвращает пару фиктивных нод с предсказуемыми тегами.
func testNodes(tags ...string) []Node {
	var out []Node
	for _, tag := range tags {
		out = append(out, Node{Tag: tag, Outbound: json.RawMessage(
			`{"type":"trojan","server":"example.com","server_port":443,"password":"x"}`)})
	}
	return out
}

// outboundByTag ищет outbound по тегу.
func outboundByTag(g generated, tag string) map[string]any {
	for _, o := range g.Outbounds {
		if o["tag"] == tag {
			return o
		}
	}
	return nil
}

// strList приводит значение поля правила к []string.
func strList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

// TestGenerateRuleOrder — golden-тест порядка правил: служебные идут первыми,
// дальше пользовательские сверху вниз, ровно как в списке.
func TestGenerateRuleOrder(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"ads.example.net"}, Action: rules.ActionBlock},
		{Enabled: false, Match: rules.MatchDomainSuffix, Values: []string{"выключено.example"}, Action: rules.ActionBlock},
		{Enabled: true, Match: rules.MatchProcess, Values: []string{"qbittorrent.exe"}, Action: rules.ActionDirect},
		{Enabled: true, Match: rules.MatchPrivate, Action: rules.ActionDirect},
		{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"youtube.com"}, Action: rules.ActionProxy},
	}}

	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg})

	want := []map[string]any{
		{"action": "sniff"},
		{"protocol": "dns", "action": "hijack-dns"},
		// Режимы Global/Direct перекрывают пользовательские правила, поэтому
		// стоят выше них.
		{"clash_mode": ModeGlobal, "outbound": ProxyTag},
		{"clash_mode": ModeDirect, "outbound": DirectTag},
		{"clash_mode": ModeRule, "action": "sniff"},
		{"domain_suffix": []string{"ads.example.net"}, "action": "reject"},
		{"process_name": []string{"qbittorrent.exe"}, "outbound": DirectTag},
		{"ip_is_private": true, "outbound": DirectTag},
		{"domain_suffix": []string{"youtube.com"}, "outbound": ProxyTag},
	}
	if len(g.Route.Rules) != len(want) {
		t.Fatalf("правил %d, ожидалось %d: %+v", len(g.Route.Rules), len(want), g.Route.Rules)
	}
	for i, w := range want {
		got := g.Route.Rules[i]
		if len(got) != len(w) {
			t.Errorf("правило %d: поля %+v, ожидались %+v", i, got, w)
			continue
		}
		for k, v := range w {
			switch exp := v.(type) {
			case []string:
				gotList := strList(got[k])
				if len(gotList) != len(exp) {
					t.Errorf("правило %d поле %s: %v, ожидалось %v", i, k, got[k], exp)
					continue
				}
				for j := range exp {
					if gotList[j] != exp[j] {
						t.Errorf("правило %d поле %s: %v, ожидалось %v", i, k, got[k], exp)
					}
				}
			default:
				if got[k] != v {
					t.Errorf("правило %d поле %s: %v, ожидалось %v", i, k, got[k], v)
				}
			}
		}
	}
	if g.Route.Final != ProxyTag {
		t.Errorf("final = %q, ожидался %q", g.Route.Final, ProxyTag)
	}
}

func TestGenerateServiceRules(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy}

	// Без TUN правило про QUIC не нужно — трафик и так идёт через mixed-порт.
	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg, BlockQUIC: true})
	for _, r := range g.Route.Rules {
		if r["protocol"] == "quic" {
			t.Fatal("вне TUN правило reject QUIC не должно появляться")
		}
	}

	// В TUN — обязано быть, иначе браузер виснет на HTTP/3.
	g = gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg,
		EnableTUN: true, BlockQUIC: true})
	found := false
	for _, r := range g.Route.Rules {
		if r["protocol"] == "quic" && r["action"] == "reject" {
			found = true
		}
	}
	if !found {
		t.Fatal("в TUN нет правила reject QUIC")
	}
}

// TestGenerateModes проверяет правила режимов Clash API: они позволяют
// переключать «всё через прокси / всё напрямую» без перезапуска ядра.
func TestGenerateModes(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy}

	raw, err := Generate(Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg,
		Mode: ModeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	var full struct {
		Experimental struct {
			ClashAPI struct {
				DefaultMode string `json:"default_mode"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	if full.Experimental.ClashAPI.DefaultMode != ModeGlobal {
		t.Fatalf("default_mode = %q, ожидался %q", full.Experimental.ClashAPI.DefaultMode, ModeGlobal)
	}

	// Неизвестный режим не должен попадать в конфиг — иначе ядро не стартует.
	var g generated
	raw, err = Generate(Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg, Mode: "чепуха"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	if full.Experimental.ClashAPI.DefaultMode != ModeRule {
		t.Fatalf("неизвестный режим не откатился к Rule: %q", full.Experimental.ClashAPI.DefaultMode)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Route.Rules[2]["clash_mode"] != ModeGlobal || g.Route.Rules[3]["clash_mode"] != ModeDirect {
		t.Fatalf("нет правил режимов: %+v", g.Route.Rules[:4])
	}

	// Без нод режим Global некуда направлять — правило должно исчезнуть.
	g = gen(t, Options{ClashSecret: "x", Routing: cfg})
	for _, r := range g.Route.Rules {
		if r["clash_mode"] == ModeGlobal {
			t.Fatalf("без нод осталось правило режима Global: %+v", r)
		}
	}
}

func TestGenerateFinalDirect(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionDirect}
	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg})
	if g.Route.Final != DirectTag {
		t.Fatalf("final = %q, ожидался %q", g.Route.Final, DirectTag)
	}
	// Прокси-outbound всё равно должен существовать: на него ссылаются правила
	// и удалённый DNS.
	if outboundByTag(g, ProxyTag) == nil {
		t.Fatal("нет outbound proxy при final=direct")
	}
	if g.DNS.Final != "dns-remote" {
		t.Fatalf("DNS final = %q, ожидался dns-remote", g.DNS.Final)
	}
}

func TestGenerateNoNodes(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"youtube.com"}, Action: rules.ActionProxy},
		{Enabled: true, Match: rules.MatchPrivate, Action: rules.ActionDirect},
	}}
	g := gen(t, Options{ClashSecret: "x", Routing: cfg})

	if g.Route.Final != DirectTag {
		t.Fatalf("без нод final = %q, ожидался direct", g.Route.Final)
	}
	// Правило на несуществующий proxy-outbound уронило бы ядро — оно должно
	// быть выброшено, а не сгенерировано.
	for _, r := range g.Route.Rules {
		if r["outbound"] == ProxyTag {
			t.Fatalf("без нод сгенерировано правило на proxy: %+v", r)
		}
	}
	if len(g.Outbounds) != 1 || g.Outbounds[0]["tag"] != DirectTag {
		t.Fatalf("без нод ожидался единственный direct: %+v", g.Outbounds)
	}
}

func TestGenerateGroups(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy,
		Groups: []rules.Group{
			{ID: "g1", Name: "Нидерланды", Type: rules.GroupURLTest, Filter: "^NL"},
			{ID: "g2", Name: "Германия", Type: rules.GroupSelect, Nodes: []string{"DE-1"}},
			{ID: "g3", Name: "Пустая", Type: rules.GroupSelect, Filter: "^JP"},
		},
		Rules: []rules.Rule{
			{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"netflix.com"},
				Action: rules.ActionProxy, Target: "Нидерланды"},
			// Ссылка на группу без нод: она не попала в конфиг, значит правило
			// должно уйти на основной селектор, а не на несуществующий outbound.
			{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"jp.example"},
				Action: rules.ActionProxy, Target: "Пустая"},
		}}

	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1", "NL-2", "DE-1"), Routing: cfg})

	nl := outboundByTag(g, "Нидерланды")
	if nl == nil || nl["type"] != "urltest" {
		t.Fatalf("группа-urltest не сгенерирована: %+v", nl)
	}
	if got := strList(nl["outbounds"]); len(got) != 2 || got[0] != "NL-1" || got[1] != "NL-2" {
		t.Fatalf("состав группы по фильтру: %v", got)
	}
	de := outboundByTag(g, "Германия")
	if de == nil || de["type"] != "selector" || de["default"] != "DE-1" {
		t.Fatalf("группа-selector не сгенерирована: %+v", de)
	}
	if outboundByTag(g, "Пустая") != nil {
		t.Fatal("группа без нод не должна попадать в конфиг")
	}

	// Группы видны в основном селекторе — сразу после auto, перед сырыми нодами.
	main := outboundByTag(g, ProxyTag)
	want := []string{AutoTag, "Нидерланды", "Германия", "NL-1", "NL-2", "DE-1"}
	got := strList(main["outbounds"])
	if len(got) != len(want) {
		t.Fatalf("состав основного селектора: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("состав основного селектора: %v, ожидался %v", got, want)
		}
	}

	last := g.Route.Rules[len(g.Route.Rules)-1]
	if last["outbound"] != ProxyTag {
		t.Fatalf("правило на пустую группу: outbound = %v, ожидался proxy", last["outbound"])
	}
}

func TestGenerateRuleSetOnlyWhenFilePresent(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{Enabled: true, Match: rules.MatchRuleSet,
			Values: []string{rules.RuleSetGeoIPRU, rules.RuleSetGeositeRU}, Action: rules.ActionDirect},
	}}

	// Каталога с .srs нет — правило выбрасываем: с несуществующим rule_set ядро
	// не стартует вовсе.
	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg})
	if len(g.Route.RuleSet) != 0 {
		t.Fatalf("без каталога ассетов rule_set не должен появляться: %+v", g.Route.RuleSet)
	}
	for _, r := range g.Route.Rules {
		if r["rule_set"] != nil {
			t.Fatalf("правило на отсутствующий .srs осталось: %+v", r)
		}
	}

	// Кладём только один из двух файлов — в конфиг попадает он один.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, rules.RuleSetGeoIPRU+".srs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g = gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg, RuleSetDir: dir})
	if len(g.Route.RuleSet) != 1 || g.Route.RuleSet[0].Tag != rules.RuleSetGeoIPRU {
		t.Fatalf("rule_set: %+v", g.Route.RuleSet)
	}
	if g.Route.RuleSet[0].Path != filepath.Join(dir, rules.RuleSetGeoIPRU+".srs") {
		t.Fatalf("путь к .srs: %q", g.Route.RuleSet[0].Path)
	}
	last := g.Route.Rules[len(g.Route.Rules)-1]
	if got := strList(last["rule_set"]); len(got) != 1 || got[0] != rules.RuleSetGeoIPRU {
		t.Fatalf("в правиле остались недоступные наборы: %v", got)
	}
}

func TestGenerateRuleFields(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name  string
		rule  rules.Rule
		check func(t *testing.T, r map[string]any)
	}{
		{"порты числами", rules.Rule{Enabled: true, Match: rules.MatchPort,
			Values: []string{"25", "465"}, Action: rules.ActionBlock},
			func(t *testing.T, r map[string]any) {
				ports, ok := r["port"].([]any)
				if !ok || len(ports) != 2 {
					t.Fatalf("port: %#v", r["port"])
				}
				// В sing-box port — числовое поле; строка «25» была бы отвергнута.
				if _, ok := ports[0].(float64); !ok {
					t.Fatalf("порт должен быть числом, получено %#v", ports[0])
				}
			}},
		{"drop вместо отказа", rules.Rule{Enabled: true, Match: rules.MatchProtocol,
			Values: []string{"quic"}, Action: rules.ActionBlock, Method: rules.RejectDrop},
			func(t *testing.T, r map[string]any) {
				if r["action"] != "reject" || r["method"] != "drop" {
					t.Fatalf("ожидался reject/drop: %+v", r)
				}
			}},
		{"фрагментация TLS", rules.Rule{Enabled: true, Match: rules.MatchDomainSuffix,
			Values: []string{"example.com"}, Action: rules.ActionDirect, TLSFragment: true},
			func(t *testing.T, r map[string]any) {
				// tls_fragment — поле действия route, поэтому action указывается явно.
				if r["action"] != "route" || r["tls_fragment"] != true || r["outbound"] != DirectTag {
					t.Fatalf("ожидался route+tls_fragment: %+v", r)
				}
				// Без явной паузы ядро берёт свои 500 мс на каждом соединении.
				if r["tls_fragment_fallback_delay"] != tlsFragmentFallbackDelay {
					t.Fatalf("ожидалась пауза %s: %+v", tlsFragmentFallbackDelay, r)
				}
			}},
		{"регулярка по домену", rules.Rule{Enabled: true, Match: rules.MatchDomainRegex,
			Values: []string{`^ads\..*\.com$`}, Action: rules.ActionBlock},
			func(t *testing.T, r map[string]any) {
				if got := strList(r["domain_regex"]); len(got) != 1 {
					t.Fatalf("domain_regex: %#v", r["domain_regex"])
				}
			}},
		{"подсеть", rules.Rule{Enabled: true, Match: rules.MatchIPCIDR,
			Values: []string{"8.8.8.8"}, Action: rules.ActionDirect},
			func(t *testing.T, r map[string]any) {
				got := strList(r["ip_cidr"])
				if len(got) != 1 || got[0] != "8.8.8.8/32" {
					t.Fatalf("ip_cidr: %v (голый IP должен получить маску)", got)
				}
			}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := gen(t, Options{ClashSecret: "x", RuleSetDir: dir, Nodes: testNodes("NL-1"),
				Routing: rules.Config{Version: rules.Version, Final: rules.ActionProxy,
					Rules: []rules.Rule{c.rule}}})
			c.check(t, g.Route.Rules[len(g.Route.Rules)-1])
		})
	}
}

// TestGenerateMigratedMatchesLegacy проверяет, что мигрированные со старых
// настроек правила дают ту же маршрутизацию, что и версии до 1.2.0.
func TestGenerateMigratedMatchesLegacy(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{rules.RuleSetGeoIPRU, rules.RuleSetGeositeRU, rules.RuleSetGeositeAds} {
		if err := os.WriteFile(filepath.Join(dir, name+".srs"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := rules.Migrate("ru-direct", true,
		[]string{"gosuslugi.ru"}, []string{"youtube.com"}, []string{"ads.example.net"})

	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg, RuleSetDir: dir})

	// sniff, hijack-dns, три правила режимов + шесть мигрированных в прежнем
	// порядке.
	if len(g.Route.Rules) != 11 {
		t.Fatalf("правил %d, ожидалось 11: %+v", len(g.Route.Rules), g.Route.Rules)
	}
	checks := []struct {
		idx   int
		field string
		out   string
	}{
		{5, "domain_suffix", ""},        // блокировка своих доменов
		{6, "domain_suffix", DirectTag}, // свои напрямую
		{7, "domain_suffix", ProxyTag},  // свои через прокси
		{8, "rule_set", ""},             // реклама
		{9, "ip_is_private", DirectTag}, // приватные сети
		{10, "rule_set", DirectTag},     // РФ напрямую
	}
	for _, c := range checks {
		r := g.Route.Rules[c.idx]
		if r[c.field] == nil {
			t.Errorf("правило %d: нет поля %s (%+v)", c.idx, c.field, r)
			continue
		}
		if c.out == "" {
			if r["action"] != "reject" {
				t.Errorf("правило %d: ожидался reject, получено %+v", c.idx, r)
			}
			continue
		}
		if r["outbound"] != c.out {
			t.Errorf("правило %d: outbound = %v, ожидался %s", c.idx, r["outbound"], c.out)
		}
	}
	if len(g.Route.RuleSet) != 3 {
		t.Errorf("ожидались все три .srs, получено %+v", g.Route.RuleSet)
	}
}

// TestGenerateRemoteRuleSet — удалённый набор описывается в routing.json и
// попадает в конфиг целиком, включая интервал обновления и способ загрузки.
func TestGenerateRemoteRuleSet(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy,
		RuleSets: []rules.RuleSet{{ID: "1", Tag: "my-list", Type: rules.SetRemote,
			URL: "https://example.com/list.srs", UpdateHours: 6, Detour: rules.ActionProxy}},
		Rules: []rules.Rule{{Enabled: true, Match: rules.MatchRuleSet,
			Values: []string{"my-list"}, Action: rules.ActionBlock}}}

	// Каталога ассетов нет, но правило должно выжить: файл и не нужен.
	g := gen(t, Options{ClashSecret: "x", Nodes: testNodes("NL-1"), Routing: cfg})
	if len(g.Route.RuleSet) != 1 {
		t.Fatalf("удалённый набор не попал в конфиг: %+v", g.Route.RuleSet)
	}
	rs := g.Route.RuleSet[0]
	if rs.Type != "remote" || rs.Tag != "my-list" || rs.URL != "https://example.com/list.srs" {
		t.Fatalf("набор описан неверно: %+v", rs)
	}
	if rs.Format != "binary" || rs.UpdateInterval != "6h" {
		t.Fatalf("формат/интервал: %+v", rs)
	}
	if rs.DownloadDetour != ProxyTag {
		t.Fatalf("загрузка должна идти через прокси, получено %q", rs.DownloadDetour)
	}
	if rs.Path != "" {
		t.Fatalf("у удалённого набора не должно быть пути: %q", rs.Path)
	}
	last := g.Route.Rules[len(g.Route.Rules)-1]
	if got := strList(last["rule_set"]); len(got) != 1 || got[0] != "my-list" {
		t.Fatalf("правило потеряло набор: %v", got)
	}
}

// TestGenerateRemoteRuleSetDetourWithoutNodes — outbound-а "proxy" без нод в
// конфиге нет, поэтому загрузку набора приходится откатывать на direct.
func TestGenerateRemoteRuleSetDetourWithoutNodes(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionDirect,
		RuleSets: []rules.RuleSet{{ID: "1", Tag: "my-list", Type: rules.SetRemote,
			URL: "https://example.com/list.srs", Detour: rules.ActionProxy}},
		Rules: []rules.Rule{{Enabled: true, Match: rules.MatchRuleSet,
			Values: []string{"my-list"}, Action: rules.ActionBlock}}}

	g := gen(t, Options{ClashSecret: "x", Routing: cfg})
	if len(g.Route.RuleSet) != 1 || g.Route.RuleSet[0].DownloadDetour != DirectTag {
		t.Fatalf("без нод загрузка должна идти напрямую: %+v", g.Route.RuleSet)
	}
}
