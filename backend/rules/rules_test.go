package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanValues(t *testing.T) {
	got := CleanValues(MatchDomainSuffix, []string{" Example.COM ", "", "example.com", "\tru.wikipedia.org"})
	want := []string{"example.com", "ru.wikipedia.org"}
	if len(got) != len(want) {
		t.Fatalf("получено %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("получено %v, ожидалось %v", got, want)
		}
	}

	// Голый IP дополняется маской, дубли схлопываются.
	ips := CleanValues(MatchIPCIDR, []string{"1.1.1.1", "1.1.1.1/32", "10.0.0.0/8"})
	if len(ips) != 2 || ips[0] != "1.1.1.1/32" || ips[1] != "10.0.0.0/8" {
		t.Fatalf("IP-нормализация: %v", ips)
	}
}

func TestRuleValidate(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		ok   bool
	}{
		{"домены", Rule{Match: MatchDomainSuffix, Values: []string{"ya.ru"}, Action: ActionProxy}, true},
		{"приватные без значений", Rule{Match: MatchPrivate, Action: ActionDirect}, true},
		{"пустые значения", Rule{Match: MatchDomainSuffix, Values: []string{" "}, Action: ActionDirect}, false},
		{"неизвестный матчер", Rule{Match: "wat", Values: []string{"x"}, Action: ActionDirect}, false},
		{"неизвестное действие", Rule{Match: MatchDomain, Values: []string{"ya.ru"}, Action: "nope"}, false},
		{"плохой порт", Rule{Match: MatchPort, Values: []string{"99999"}, Action: ActionBlock}, false},
		{"хороший порт", Rule{Match: MatchPort, Values: []string{"443"}, Action: ActionBlock}, true},
		{"плохая подсеть", Rule{Match: MatchIPCIDR, Values: []string{"300.1.1.1"}, Action: ActionDirect}, false},
		{"плохая регулярка", Rule{Match: MatchDomainRegex, Values: []string{"("}, Action: ActionDirect}, false},
		{"drop только для блока", Rule{Match: MatchDomain, Values: []string{"ya.ru"}, Action: ActionDirect, Method: RejectDrop}, false},
		{"drop для блока", Rule{Match: MatchProtocol, Values: []string{"quic"}, Action: ActionBlock, Method: RejectDrop}, true},
		{"фрагмент не для блока", Rule{Match: MatchDomain, Values: []string{"ya.ru"}, Action: ActionBlock, TLSFragment: true}, false},
		{"фрагмент не для прокси", Rule{Match: MatchDomain, Values: []string{"ya.ru"}, Action: ActionProxy, TLSFragment: true}, false},
		{"фрагмент для direct", Rule{Match: MatchDomain, Values: []string{"ya.ru"}, Action: ActionDirect, TLSFragment: true}, true},
		{"неизвестный протокол", Rule{Match: MatchProtocol, Values: []string{"gopher"}, Action: ActionBlock}, false},
	}
	for _, c := range cases {
		err := c.rule.Validate()
		if (err == nil) != c.ok {
			t.Errorf("%s: Validate() = %v, ожидалось ok=%v", c.name, err, c.ok)
		}
	}
}

func TestConfigValidateGroupRefs(t *testing.T) {
	c := Config{
		Version: Version, Final: ActionProxy,
		Groups: []Group{{ID: "g1", Name: "Стриминг", Type: GroupSelect}},
		Rules: []Rule{
			{ID: "r1", Enabled: true, Match: MatchDomainSuffix, Values: []string{"netflix.com"}, Action: ActionProxy, Target: "Стриминг"},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("валидный конфиг отвергнут: %v", err)
	}

	c.Rules[0].Target = "Нет такой"
	if err := c.Validate(); err == nil {
		t.Fatal("ссылка на несуществующую группу должна отвергаться")
	}

	// Зарезервированное имя группы.
	c.Rules[0].Target = ""
	c.Groups[0].Name = "direct"
	if err := c.Validate(); err == nil {
		t.Fatal("зарезервированное имя группы должно отвергаться")
	}
}

func TestGroupMatchNodes(t *testing.T) {
	all := []string{"NL-1", "NL-2", "DE-1"}
	g := Group{Name: "Нидерланды", Type: GroupURLTest, Filter: "^NL"}
	got := g.MatchNodes(all)
	if len(got) != 2 || got[0] != "NL-1" || got[1] != "NL-2" {
		t.Fatalf("фильтр: %v", got)
	}

	// Явный список: порядок берётся из профиля, лишние теги игнорируются.
	g = Group{Name: "Свои", Type: GroupSelect, Nodes: []string{"DE-1", "нет такой", "NL-1"}}
	got = g.MatchNodes(all)
	if len(got) != 2 || got[0] != "NL-1" || got[1] != "DE-1" {
		t.Fatalf("явный список: %v", got)
	}
}

func TestMigrateOrder(t *testing.T) {
	c := Migrate("ru-direct", true,
		[]string{"gosuslugi.ru"}, []string{"youtube.com"}, []string{"ads.example.net"})
	if err := c.Validate(); err != nil {
		t.Fatalf("мигрированный конфиг невалиден: %v", err)
	}

	// Порядок повторяет прежний generator.go: блок → напрямую → прокси →
	// реклама → приватные → РФ.
	want := []struct {
		action string
		match  string
	}{
		{ActionBlock, MatchDomainSuffix},
		{ActionDirect, MatchDomainSuffix},
		{ActionProxy, MatchDomainSuffix},
		{ActionBlock, MatchRuleSet},
		{ActionDirect, MatchPrivate},
		{ActionDirect, MatchRuleSet},
	}
	if len(c.Rules) != len(want) {
		t.Fatalf("правил %d, ожидалось %d: %+v", len(c.Rules), len(want), c.Rules)
	}
	for i, w := range want {
		if c.Rules[i].Action != w.action || c.Rules[i].Match != w.match {
			t.Errorf("правило %d: %s/%s, ожидалось %s/%s", i,
				c.Rules[i].Action, c.Rules[i].Match, w.action, w.match)
		}
		if !c.Rules[i].Enabled {
			t.Errorf("правило %d выключено, а должно быть включено", i)
		}
	}
	if c.Final != ActionProxy {
		t.Errorf("final = %q", c.Final)
	}
}

func TestMigrateDisablesOptional(t *testing.T) {
	c := Migrate("global", false, nil, nil, nil)
	if len(c.Rules) != 3 {
		t.Fatalf("ожидались только встроенные правила, получено %d", len(c.Rules))
	}
	for _, r := range c.Rules {
		if !r.Builtin {
			t.Errorf("правило %q должно быть встроенным", r.Title())
		}
		want := r.Match == MatchPrivate // включены только приватные сети
		if r.Enabled != want {
			t.Errorf("правило %q: enabled=%v, ожидалось %v", r.Title(), r.Enabled, want)
		}
	}
}

func TestStoreRoundTripAndCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Exists() {
		t.Fatal("файла быть не должно")
	}
	if err := s.Init(Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "routing.json")); err != nil {
		t.Fatalf("routing.json не создан: %v", err)
	}
	// Повторный Init ничего не перезаписывает.
	if err := s.Init(Migrate("ru-direct", true, nil, nil, nil)); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Get().Rules {
		if r.Match == MatchRuleSet && r.Enabled {
			t.Fatal("повторный Init перезаписал конфиг")
		}
	}

	id, err := s.AddRule(Rule{Name: "Торренты", Enabled: true,
		Match: MatchProcess, Values: []string{"qbittorrent.exe"}, Action: ActionDirect})
	if err != nil {
		t.Fatal(err)
	}

	// Невалидное правило не должно попадать ни в память, ни на диск.
	if _, err := s.AddRule(Rule{Match: MatchPort, Values: []string{"нет"}, Action: ActionDirect}); err == nil {
		t.Fatal("невалидное правило принято")
	}
	if n := len(s.Get().Rules); n != 4 {
		t.Fatalf("после отката ожидалось 4 правила, получено %d", n)
	}

	if err := s.MoveRule(id, 0); err != nil {
		t.Fatal(err)
	}
	if s.Get().Rules[0].ID != id {
		t.Fatal("MoveRule не переставил правило в начало")
	}

	// Встроенное правило удалить нельзя.
	var builtinID string
	for _, r := range s.Get().Rules {
		if r.Builtin {
			builtinID = r.ID
			break
		}
	}
	if err := s.DeleteRule(builtinID); err == nil {
		t.Fatal("встроенное правило удалилось")
	}
	if err := s.DeleteRule(id); err != nil {
		t.Fatal(err)
	}

	// Перечитываем с диска — состояние должно совпасть.
	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Exists() {
		t.Fatal("файл не найден после сохранения")
	}
	if len(s2.Get().Rules) != len(s.Get().Rules) {
		t.Fatalf("на диске %d правил, в памяти %d", len(s2.Get().Rules), len(s.Get().Rules))
	}
}

func TestStoreGroupRename(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.Init(Default())

	gid, err := s.AddGroup(Group{Name: "Стриминг", Type: GroupSelect, Filter: "NL"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule(Rule{Enabled: true, Match: MatchDomainSuffix,
		Values: []string{"netflix.com"}, Action: ActionProxy, Target: "Стриминг"}); err != nil {
		t.Fatal(err)
	}

	// Переименование группы тянет за собой ссылки правил.
	if err := s.UpdateGroup(Group{ID: gid, Name: "Медиа", Type: GroupSelect, Filter: "NL"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range s.Get().Rules {
		if r.Target == "Медиа" {
			found = true
		}
		if r.Target == "Стриминг" {
			t.Fatal("осталась ссылка на старое имя группы")
		}
	}
	if !found {
		t.Fatal("ссылка правила не обновилась на новое имя")
	}

	// Удаление группы переводит правила на основной селектор.
	if err := s.DeleteGroup(gid); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Get().Rules {
		if r.Target != "" {
			t.Fatalf("правило %q ссылается на удалённую группу", r.Title())
		}
	}
}

func TestStoreCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "routing.json"), []byte("{это не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Битый файл — это ошибка, о которой пользователю надо сообщить, но не повод
	// остаться без хранилища: Load обязан вернуть рабочий Store с дефолтами.
	s, err := Load(dir)
	if err == nil {
		t.Fatal("битый файл должен возвращать ошибку — иначе о потере правил никто не узнает")
	}
	if s == nil {
		t.Fatal("Load вернул nil-хранилище: приложение упадёт на первом же обращении")
	}
	if s.Exists() {
		t.Fatal("битый файл должен считаться отсутствующим, чтобы Init его перезаписал")
	}
	if len(s.Get().Rules) == 0 {
		t.Fatal("после битого файла ожидались дефолтные правила")
	}
	// Прежнее содержимое отложено рядом: Init вот-вот перезапишет routing.json,
	// и без копии разобраться, что было у пользователя, будет негде.
	bad, rerr := os.ReadFile(filepath.Join(dir, "routing.json.bad"))
	if rerr != nil {
		t.Fatalf("битый файл не сохранён как routing.json.bad: %v", rerr)
	}
	if string(bad) != "{это не json" {
		t.Fatalf("в routing.json.bad не исходное содержимое: %q", bad)
	}
}

// TestNormalizeDropsLegacyFragment — до 2.0.0 UI разрешал фрагментацию TLS и на
// прокси-правилах. Такой флаг обязан гаситься при загрузке: иначе строгий
// Validate уронил бы первое же сохранение правил на существующем routing.json.
func TestNormalizeDropsLegacyFragment(t *testing.T) {
	c := Config{
		Version: Version, Final: ActionProxy,
		Rules: []Rule{
			{ID: "a", Enabled: true, Match: MatchDomain, Values: []string{"ya.ru"},
				Action: ActionProxy, TLSFragment: true},
			{ID: "b", Enabled: true, Match: MatchDomain, Values: []string{"vk.com"},
				Action: ActionDirect, TLSFragment: true},
		},
	}
	c.Normalize()
	if c.Rules[0].TLSFragment {
		t.Error("на прокси-правиле флаг фрагментации должен гаснуть")
	}
	if !c.Rules[1].TLSFragment {
		t.Error("на direct-правиле флаг фрагментации должен сохраняться")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("нормализованный конфиг обязан проходить Validate: %v", err)
	}
}
