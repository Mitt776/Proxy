//go:build coretest

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"Proxy/backend/rules"
)

// cores возвращает все ядра из каталога ассетов. Проверять надо оба: штатное
// sing-box и форк с XHTTP — их схемы конфига расходятся, и поле, принятое одним,
// может уронить другое.
func cores(t *testing.T) map[string]string {
	t.Helper()
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан")
	}
	out := map[string]string{}
	for _, name := range []string{"sing-box.exe", "sing-box-xhttp.exe"} {
		path := filepath.Join(assets, name)
		if _, err := os.Stat(path); err == nil {
			out[name] = path
		}
	}
	if len(out) == 0 {
		t.Skipf("в %s нет ни одного ядра", assets)
	}
	return out
}

// checkConfig прогоняет конфиг через `check` каждого ядра.
func checkConfig(t *testing.T, opts Options) {
	t.Helper()
	assets := os.Getenv("PROXY_ASSETS")
	opts.RuleSetDir = assets
	opts.CacheDBPath = "cache.db"
	opts.ClashSecret = "x"

	cfg, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, bin := range cores(t) {
		out, err := exec.Command(bin, "check", "-c", cfgPath, "-D", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("%s check ПРОВАЛИЛСЯ: %v\n%s\n---config---\n%s", name, err, out, cfg)
		}
	}
}

func ruleTestNode(t *testing.T) Node {
	t.Helper()
	n, err := ParseLink("trojan://pw@tr.example.com:443?sni=tr.example.com#NL-1")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestMigratedRulesValidate — конфиг, мигрированный со старых настроек, должен
// приниматься ядром так же, как принимался раньше.
func TestMigratedRulesValidate(t *testing.T) {
	checkConfig(t, Options{
		Nodes: []Node{ruleTestNode(t)},
		Routing: rules.Migrate("ru-direct", true,
			[]string{"gosuslugi.ru", "  LOCAL.dev "},
			[]string{"youtube.com"},
			[]string{"ads.example.net"}),
	})
	t.Log("✅ мигрированные правила (РФ напрямую + реклама + свои домены) валидны")
}

// TestNewRuleFieldsValidate прогоняет через ядра всё, что появилось в 1.2.0:
// новые матчеры, reject method=drop и tls_fragment. Документации тут верить
// нельзя — только вывод самого ядра.
func TestNewRuleFieldsValidate(t *testing.T) {
	cases := []struct {
		name string
		rule rules.Rule
	}{
		{"process_name", rules.Rule{Enabled: true, Match: rules.MatchProcess,
			Values: []string{"qbittorrent.exe"}, Action: rules.ActionDirect}},
		{"process_path", rules.Rule{Enabled: true, Match: rules.MatchProcessPath,
			Values: []string{`C:\Program Files\qBittorrent\qbittorrent.exe`}, Action: rules.ActionDirect}},
		{"domain_regex", rules.Rule{Enabled: true, Match: rules.MatchDomainRegex,
			Values: []string{`^ads\..*\.com$`}, Action: rules.ActionBlock}},
		{"domain_keyword", rules.Rule{Enabled: true, Match: rules.MatchDomainKeyword,
			Values: []string{"tracker"}, Action: rules.ActionBlock}},
		{"ip_cidr", rules.Rule{Enabled: true, Match: rules.MatchIPCIDR,
			Values: []string{"8.8.8.8", "10.0.0.0/8"}, Action: rules.ActionDirect}},
		{"port", rules.Rule{Enabled: true, Match: rules.MatchPort,
			Values: []string{"25", "465"}, Action: rules.ActionBlock}},
		{"network", rules.Rule{Enabled: true, Match: rules.MatchNetwork,
			Values: []string{"udp"}, Action: rules.ActionDirect}},
		{"protocol", rules.Rule{Enabled: true, Match: rules.MatchProtocol,
			Values: []string{"bittorrent"}, Action: rules.ActionDirect}},
		{"reject method=drop", rules.Rule{Enabled: true, Match: rules.MatchProtocol,
			Values: []string{"quic"}, Action: rules.ActionBlock, Method: rules.RejectDrop}},
		{"tls_fragment direct", rules.Rule{Enabled: true, Match: rules.MatchDomainSuffix,
			Values: []string{"example.com"}, Action: rules.ActionDirect, TLSFragment: true}},
		{"tls_fragment proxy", rules.Rule{Enabled: true, Match: rules.MatchDomainSuffix,
			Values: []string{"example.com"}, Action: rules.ActionProxy, TLSFragment: true}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkConfig(t, Options{
				Nodes: []Node{ruleTestNode(t)},
				Routing: rules.Config{Version: rules.Version, Final: rules.ActionProxy,
					Rules: []rules.Rule{c.rule}},
			})
		})
	}
}

// TestGroupsValidate проверяет, что группы нод дают валидные selector/urltest
// и что ссылка правила на группу принимается ядром.
func TestGroupsValidate(t *testing.T) {
	node := ruleTestNode(t)
	other, err := ParseLink("trojan://pw@de.example.com:443?sni=de.example.com#DE-1")
	if err != nil {
		t.Fatal(err)
	}
	checkConfig(t, Options{
		Nodes: []Node{node, other},
		Routing: rules.Config{Version: rules.Version, Final: rules.ActionProxy,
			Groups: []rules.Group{
				{ID: "g1", Name: "Нидерланды", Type: rules.GroupURLTest, Filter: "^NL"},
				{ID: "g2", Name: "Германия", Type: rules.GroupSelect, Nodes: []string{"DE-1"}},
			},
			Rules: []rules.Rule{
				{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"netflix.com"},
					Action: rules.ActionProxy, Target: "Нидерланды"},
				{Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"ard.de"},
					Action: rules.ActionProxy, Target: "Германия"},
			}},
	})
	t.Log("✅ группы нод и ссылки правил на них валидны")
}

// TestFinalDirectValidates — сплит-туннель наоборот: по умолчанию всё напрямую,
// через прокси идёт только то, что перечислено правилами.
func TestFinalDirectValidates(t *testing.T) {
	checkConfig(t, Options{
		Nodes: []Node{ruleTestNode(t)},
		Routing: rules.Config{Version: rules.Version, Final: rules.ActionDirect,
			Rules: []rules.Rule{
				{Enabled: true, Match: rules.MatchDomainSuffix,
					Values: []string{"youtube.com"}, Action: rules.ActionProxy},
			}},
	})
	t.Log("✅ конфиг с final=direct валиден")
}

// TestClashModesValidate проверяет, что переключатель режимов (clash_mode в
// правилах + default_mode в clash_api) принимается обоими ядрами.
func TestClashModesValidate(t *testing.T) {
	for _, mode := range []string{ModeRule, ModeGlobal, ModeDirect} {
		t.Run(mode, func(t *testing.T) {
			checkConfig(t, Options{
				Nodes: []Node{ruleTestNode(t)},
				Mode:  mode,
				Routing: rules.Config{Version: rules.Version, Final: rules.ActionProxy,
					Rules: []rules.Rule{{Enabled: true, Match: rules.MatchDomainSuffix,
						Values: []string{"youtube.com"}, Action: rules.ActionProxy}}},
			})
		})
	}
}

// TestRemoteRuleSetValidates — удалённые наборы правил (1.3.0). Проверяем, что
// оба ядра принимают полный набор полей: url, format, download_detour и
// update_interval, причём detour как direct, так и через прокси.
func TestRemoteRuleSetValidates(t *testing.T) {
	sets := []rules.RuleSet{
		{ID: "1", Tag: "remote-binary", Type: rules.SetRemote,
			URL:    "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-github.srs",
			Format: rules.FormatBinary, UpdateHours: 12, Detour: rules.ActionDirect},
		{ID: "2", Tag: "remote-source", Type: rules.SetRemote,
			URL:    "https://example.com/list.json",
			Format: rules.FormatSource, Detour: rules.ActionProxy},
	}
	routing := rules.Config{Version: rules.Version, Final: rules.ActionProxy, RuleSets: sets, Rules: []rules.Rule{
		{ID: "a", Enabled: true, Name: "Удалённый список — через прокси",
			Match: rules.MatchRuleSet, Values: []string{"remote-binary"}, Action: rules.ActionProxy},
		{ID: "b", Enabled: true, Name: "Удалённый список — блок",
			Match: rules.MatchRuleSet, Values: []string{"remote-source"}, Action: rules.ActionBlock},
		{ID: "c", Enabled: true, Name: "Локальный и удалённый вместе",
			Match: rules.MatchRuleSet, Values: []string{rules.RuleSetGeositeRU, "remote-binary"}, Action: rules.ActionDirect},
	}}
	checkConfig(t, Options{Nodes: []Node{ruleTestNode(t)}, Routing: routing})
	t.Log("✅ remote rule_set (binary/source, detour direct/proxy, update_interval) принят обоими ядрами")
}

// TestRemoteRuleSetDetourFallsBackWithoutNodes — без нод outbound-а "proxy" в
// конфиге нет, и download_detour: proxy сделал бы конфиг невалидным.
func TestRemoteRuleSetDetourFallsBackWithoutNodes(t *testing.T) {
	routing := rules.Config{Version: rules.Version, Final: rules.ActionDirect,
		RuleSets: []rules.RuleSet{{ID: "1", Tag: "remote-x", Type: rules.SetRemote,
			URL: "https://example.com/list.srs", Detour: rules.ActionProxy}},
		Rules: []rules.Rule{{ID: "a", Enabled: true, Match: rules.MatchRuleSet,
			Values: []string{"remote-x"}, Action: rules.ActionBlock}}}
	checkConfig(t, Options{Routing: routing})
	t.Log("✅ без нод загрузка набора откатывается на direct")
}

// TestLogLevelsValidate — все уровни журнала, которые предлагает UI.
func TestLogLevelsValidate(t *testing.T) {
	for _, level := range []string{"trace", "debug", "info", "warn", "error"} {
		checkConfig(t, Options{LogLevel: level, Nodes: []Node{ruleTestNode(t)},
			Routing: rules.Default()})
	}
	t.Log("✅ уровни журнала trace…error приняты обоими ядрами")
}
