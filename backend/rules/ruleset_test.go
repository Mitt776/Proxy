package rules

import (
	"strings"
	"testing"
)

func setStore(t *testing.T) *Store {
	t.Helper()
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(Config{Version: Version, Final: ActionProxy}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRuleSetValidation(t *testing.T) {
	bad := []RuleSet{
		{Tag: "", Type: SetRemote, URL: "https://e.com/x.srs"},
		{Tag: "с пробелом", Type: SetRemote, URL: "https://e.com/x.srs"},
		{Tag: "x", Type: SetRemote, URL: "ftp://e.com/x.srs"},
		{Tag: "x", Type: SetRemote, URL: ""},
		{Tag: "x", Type: "странный", URL: "https://e.com/x.srs"},
		{Tag: "x", Type: SetRemote, URL: "https://e.com/x.srs", Format: "yaml"},
		{Tag: "x", Type: SetRemote, URL: "https://e.com/x.srs", Detour: "куда-нибудь"},
	}
	for _, rs := range bad {
		if err := rs.Validate(); err == nil {
			t.Errorf("набор %+v должен был не пройти валидацию", rs)
		}
	}
	ok := RuleSet{Tag: "my-list", Type: SetRemote, URL: "https://e.com/x.srs",
		Format: FormatSource, Detour: ActionProxy, UpdateHours: 6}
	if err := ok.Validate(); err != nil {
		t.Fatalf("корректный набор отвергнут: %v", err)
	}
	if ok.UpdateInterval() != "6h" {
		t.Fatalf("интервал %q, ожидался 6h", ok.UpdateInterval())
	}
	empty := RuleSet{Tag: "x", Type: SetRemote, URL: "https://e.com/x.srs"}
	if empty.UpdateInterval() != "24h" || empty.FormatOrDefault() != FormatBinary {
		t.Fatalf("умолчания набора неверны: %s / %s", empty.UpdateInterval(), empty.FormatOrDefault())
	}
}

func TestRuleSetDuplicateTagRejected(t *testing.T) {
	s := setStore(t)
	first := RuleSet{Tag: "list", Type: SetRemote, URL: "https://e.com/a.srs"}
	if _, err := s.AddRuleSet(first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRuleSet(RuleSet{Tag: "list", Type: SetRemote, URL: "https://e.com/b.srs"}); err == nil {
		t.Fatal("два набора с одним тегом приняты")
	}
	if got := len(s.Get().RuleSets); got != 1 {
		t.Fatalf("после отказа наборов %d, ожидался 1", got)
	}
}

func TestRuleSetRenamePropagatesToRules(t *testing.T) {
	s := setStore(t)
	id, err := s.AddRuleSet(RuleSet{Tag: "old-list", Type: SetRemote, URL: "https://e.com/a.srs"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule(Rule{Enabled: true, Match: MatchRuleSet,
		Values: []string{"old-list", RuleSetGeositeRU}, Action: ActionBlock}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRuleSet(RuleSet{ID: id, Tag: "new-list", Type: SetRemote, URL: "https://e.com/a.srs"}); err != nil {
		t.Fatal(err)
	}
	vals := s.Get().Rules[0].Values
	if vals[0] != "new-list" || vals[1] != RuleSetGeositeRU {
		t.Fatalf("после переименования правило ссылается на %v", vals)
	}
}

func TestRuleSetDeleteBlockedWhileUsed(t *testing.T) {
	s := setStore(t)
	id, err := s.AddRuleSet(RuleSet{Tag: "list", Type: SetRemote, URL: "https://e.com/a.srs"})
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := s.AddRule(Rule{Name: "Мой блок", Enabled: true, Match: MatchRuleSet,
		Values: []string{"list"}, Action: ActionBlock})
	if err != nil {
		t.Fatal(err)
	}
	err = s.DeleteRuleSet(id)
	if err == nil {
		t.Fatal("набор удалён, хотя на него ссылается правило")
	}
	if !strings.Contains(err.Error(), "Мой блок") {
		t.Fatalf("в ошибке нет имени правила: %v", err)
	}
	if err := s.DeleteRule(ruleID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRuleSet(id); err != nil {
		t.Fatalf("свободный набор не удалился: %v", err)
	}
	if len(s.Get().RuleSets) != 0 {
		t.Fatal("набор остался в конфиге")
	}
}

func TestRuleSetSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(Config{Version: Version, Final: ActionProxy}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRuleSet(RuleSet{Tag: "list", Type: SetRemote,
		URL: "https://e.com/a.srs", Detour: ActionProxy, UpdateHours: 3}); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := again.Get()
	got := cfg.FindRuleSet("list")
	if got == nil {
		t.Fatal("набор не сохранился на диск")
	}
	if got.Detour != ActionProxy || got.UpdateHours != 3 {
		t.Fatalf("поля набора потерялись: %+v", got)
	}
}
