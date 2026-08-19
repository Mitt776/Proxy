package config

import (
	"strings"
	"testing"

	"Proxy/backend/rules"
)

// fakeMatcher подменяет ядро: для source-набора считает совпавшими правила,
// чьи значения перечислены в hits, для .srs — сверяется по имени файла.
func fakeMatcher(t *testing.T, srcHits []int, setHits map[string]bool) RuleSetMatcher {
	t.Helper()
	return func(path, format, domain string) ([]int, error) {
		if format == rules.FormatSource {
			return srcHits, nil
		}
		for tag, ok := range setHits {
			if ok && strings.Contains(path, tag) {
				return []int{0}, nil
			}
		}
		return nil, nil
	}
}

func checkOpts(cfg rules.Config) Options {
	return Options{Routing: cfg, Mode: ModeRule, RuleSetDir: "."}
}

func TestCheckDomainFirstMatchWins(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{ID: "a", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"ads.example"}, Action: rules.ActionBlock},
		{ID: "b", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"example"}, Action: rules.ActionDirect},
	}}
	// Ядро «сообщает», что совпали оба доменных правила (индексы 0 и 1 в наборе).
	got, err := CheckDomain(checkOpts(cfg), "ads.example", fakeMatcher(t, []int{0, 1}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleID != "a" || got.Action != rules.ActionBlock {
		t.Fatalf("сработать должно первое правило, получено %+v", got)
	}
	if got.ByFinal {
		t.Fatal("решение принял final, хотя правило совпало")
	}
}

func TestCheckDomainFallsToFinal(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionDirect, Rules: []rules.Rule{
		{ID: "a", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"other.test"}, Action: rules.ActionProxy},
	}}
	got, err := CheckDomain(checkOpts(cfg), "https://example.com:443/path", fakeMatcher(t, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != "example.com" {
		t.Fatalf("домен не выделен из ссылки: %q", got.Domain)
	}
	if !got.ByFinal || got.Action != rules.ActionDirect {
		t.Fatalf("ожидался final=direct, получено %+v", got)
	}
}

func TestCheckDomainSkipsUnknowable(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{ID: "ip", Enabled: true, Match: rules.MatchIPCIDR, Values: []string{"10.0.0.0/8"}, Action: rules.ActionDirect},
		{ID: "proc", Enabled: true, Match: rules.MatchProcess, Values: []string{"chrome.exe"}, Action: rules.ActionDirect},
		{ID: "dom", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"example.com"}, Action: rules.ActionProxy},
	}}
	got, err := CheckDomain(checkOpts(cfg), "example.com", fakeMatcher(t, []int{0}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleID != "dom" {
		t.Fatalf("ожидалось доменное правило, получено %q", got.RuleID)
	}
	var skipped int
	for _, s := range got.Steps {
		if s.Status == CheckSkip {
			skipped++
			if s.Reason == "" {
				t.Fatalf("пропуск правила %q без объяснения", s.RuleID)
			}
		}
	}
	if skipped != 2 {
		t.Fatalf("правил по IP/процессу пропущено %d, ожидалось 2", skipped)
	}
}

func TestCheckDomainRemoteSetIsUnknown(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy,
		RuleSets: []rules.RuleSet{{ID: "s", Tag: "my-list", Type: rules.SetRemote, URL: "https://example.com/x.srs"}},
		Rules: []rules.Rule{
			{ID: "set", Enabled: true, Match: rules.MatchRuleSet, Values: []string{"my-list"}, Action: rules.ActionProxy},
		}}
	got, err := CheckDomain(checkOpts(cfg), "example.com", fakeMatcher(t, nil, map[string]bool{"my-list": true}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Status != CheckUnknown {
		t.Fatalf("удалённый набор должен помечаться непроверяемым, получено %+v", got.Steps)
	}
	if !got.ByFinal {
		t.Fatal("непроверяемое правило не должно считаться сработавшим")
	}
}

func TestCheckDomainModesShortCircuit(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionDirect, Rules: []rules.Rule{
		{ID: "a", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"example.com"}, Action: rules.ActionBlock},
	}}
	for mode, want := range map[string]string{ModeGlobal: rules.ActionProxy, ModeDirect: rules.ActionDirect} {
		opts := checkOpts(cfg)
		opts.Mode = mode
		got, err := CheckDomain(opts, "example.com", fakeMatcher(t, []int{0}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if got.Action != want || !got.ByFinal || len(got.Steps) != 0 {
			t.Fatalf("режим %s: ожидалось %s без разбора правил, получено %+v", mode, want, got)
		}
	}
}

func TestCheckDomainIgnoresDisabled(t *testing.T) {
	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{ID: "off", Enabled: false, Match: rules.MatchDomainSuffix, Values: []string{"example.com"}, Action: rules.ActionBlock},
		{ID: "on", Enabled: true, Match: rules.MatchDomainSuffix, Values: []string{"example.com"}, Action: rules.ActionDirect},
	}}
	// Выключенное правило не попадает в набор, поэтому индекс 0 — уже второе.
	got, err := CheckDomain(checkOpts(cfg), "example.com", fakeMatcher(t, []int{0}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleID != "on" {
		t.Fatalf("выключенное правило не должно участвовать, получено %q", got.RuleID)
	}
}
