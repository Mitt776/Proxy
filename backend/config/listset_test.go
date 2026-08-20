package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseDomainList — разбор текстового списка доменов (*.lst).
// Формат живой: первая строка настоящего inside-raw.lst — суффикс «.ua»,
// остальные строки голые хосты.
func TestParseDomainList(t *testing.T) {
	raw := strings.Join([]string{
		"# комментарий",
		"! тоже комментарий",
		"",
		".ua",
		"4pda.ru",
		"4pda.ru", // дубль
		"abs.twimg.com",
		"||ads.example.com^",
		"*.wildcard.net",
		"  spaced.example.org  ",
		"UPPER.Example.COM",
		"https://example.com/path", // не домен
		"localhost",                // одиночная метка
		"tail.example.net # хвостовой комментарий",
	}, "\n")

	domains, suffixes := ParseDomainList([]byte(raw))

	wantDomains := []string{
		"4pda.ru", "abs.twimg.com", "ads.example.com",
		"spaced.example.org", "upper.example.com", "tail.example.net",
	}
	if got := strings.Join(domains, ","); got != strings.Join(wantDomains, ",") {
		t.Fatalf("точные домены:\n получили %q\n ожидали  %q", got, strings.Join(wantDomains, ","))
	}

	// «.ua» и «*.wildcard.net» дают только суффиксы, голые имена — ещё и суффикс
	// с точкой, иначе поддомены не ловились бы.
	wantSuffixes := []string{
		".ua", ".4pda.ru", ".abs.twimg.com", ".ads.example.com", ".wildcard.net",
		".spaced.example.org", ".upper.example.com", ".tail.example.net",
	}
	if got := strings.Join(suffixes, ","); got != strings.Join(wantSuffixes, ",") {
		t.Fatalf("суффиксы:\n получили %q\n ожидали  %q", got, strings.Join(wantSuffixes, ","))
	}
}

// TestBuildSourceRuleSet — точные имена и суффиксы обязаны лежать в разных
// правилах: поля внутри одного правила ядро объединяет по «и».
func TestBuildSourceRuleSet(t *testing.T) {
	data, err := BuildSourceRuleSet([]string{"a.com"}, []string{".a.com"})
	if err != nil {
		t.Fatal(err)
	}
	var got sourceRuleSet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 {
		t.Fatalf("version = %d, ожидалась 3", got.Version)
	}
	if len(got.Rules) != 2 {
		t.Fatalf("правил %d, ожидалось 2: %s", len(got.Rules), data)
	}
	if len(got.Rules[0].DomainSuffix) != 0 || len(got.Rules[1].Domain) != 0 {
		t.Fatalf("условия смешались в одном правиле: %s", data)
	}
}

// TestBuildSourceRuleSetEmpty — пустой список не должен превращаться в набор:
// ядро принимает «rules: []», но такое правило молча не срабатывает никогда.
func TestBuildSourceRuleSetEmpty(t *testing.T) {
	if _, err := BuildSourceRuleSet(nil, nil); err == nil {
		t.Fatal("пустой список принят без ошибки")
	}
}
