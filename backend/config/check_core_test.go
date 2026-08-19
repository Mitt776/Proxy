//go:build coretest

// Проверка домена на настоящем ядре: временный source-набор, который собирает
// CheckDomain, должен приниматься `sing-box rule-set match`, а его семантика
// (domain_suffix, keyword, regex, .srs) — совпадать с боевой.
//
// Запуск:
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	go test -tags coretest ./backend/config -run TestCheckDomainOnRealCore -v
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"Proxy/backend/rules"
)

// coreMatcher — тот же вызов, что делает backend/core, но напрямую бинарём.
func coreMatcher(t *testing.T, bin string) RuleSetMatcher {
	t.Helper()
	re := regexp.MustCompile(`rules\.\[(\d+)\]`)
	return func(path, format, domain string) ([]int, error) {
		out, err := exec.Command(bin, "rule-set", "match", path, domain, "-f", format).CombinedOutput()
		if err != nil {
			return nil, err
		}
		var idx []int
		for _, line := range strings.Split(string(out), "\n") {
			if m := re.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[1])
				idx = append(idx, n)
			}
		}
		return idx, nil
	}
}

func TestCheckDomainOnRealCore(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан")
	}
	if _, err := os.Stat(filepath.Join(assets, rules.RuleSetGeositeRU+".srs")); err != nil {
		t.Skipf("нет geosite-ru.srs в %s", assets)
	}

	cfg := rules.Config{Version: rules.Version, Final: rules.ActionProxy, Rules: []rules.Rule{
		{ID: "ads", Enabled: true, Name: "Реклама", Match: rules.MatchDomainKeyword,
			Values: []string{"doubleclick"}, Action: rules.ActionBlock},
		{ID: "off", Enabled: false, Name: "Выключенное", Match: rules.MatchDomainSuffix,
			Values: []string{"youtube.com"}, Action: rules.ActionDirect},
		{ID: "yt", Enabled: true, Name: "YouTube через прокси", Match: rules.MatchDomainSuffix,
			Values: []string{"youtube.com"}, Action: rules.ActionProxy},
		{ID: "re", Enabled: true, Name: "Регулярка", Match: rules.MatchDomainRegex,
			Values: []string{`^cdn\d+\.test$`}, Action: rules.ActionDirect},
		{ID: "ru", Enabled: true, Name: "Россия напрямую", Match: rules.MatchRuleSet,
			Values: []string{rules.RuleSetGeositeRU}, Action: rules.ActionDirect},
	}}

	for name, bin := range cores(t) {
		match := coreMatcher(t, bin)
		opts := Options{Routing: cfg, Mode: ModeRule, RuleSetDir: assets}

		cases := []struct{ domain, rule, action string }{
			{"ads.doubleclick.net", "ads", rules.ActionBlock},
			{"www.youtube.com", "yt", rules.ActionProxy}, // поддомен ловится суффиксом
			{"youtube.com", "yt", rules.ActionProxy},     // и сам домен тоже
			{"cdn42.test", "re", rules.ActionDirect},     // регулярка
			{"yandex.ru", "ru", rules.ActionDirect},      // локальный .srs
			{"wikipedia.org", "", rules.ActionProxy},     // никуда не попал — final
		}
		for _, c := range cases {
			got, err := CheckDomain(opts, c.domain, match)
			if err != nil {
				t.Fatalf("%s: CheckDomain(%s): %v", name, c.domain, err)
			}
			if got.RuleID != c.rule || got.Action != c.action {
				t.Fatalf("%s: %s → правило %q действие %q, ожидалось %q/%q",
					name, c.domain, got.RuleID, got.Action, c.rule, c.action)
			}
			if c.rule == "" && !got.ByFinal {
				t.Fatalf("%s: %s должен был уйти в final", name, c.domain)
			}
		}
	}
	t.Log("✅ проверка домена совпадает с семантикой ядра (суффикс, ключевое слово, регулярка, .srs)")
}
