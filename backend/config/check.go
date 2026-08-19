package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Proxy/backend/rules"
)

// RuleSetMatcher проверяет набор правил (файл .srs или source-JSON) против
// домена и возвращает индексы совпавших правил внутри набора. Реализуется
// через `sing-box rule-set match` в backend/core — здесь только схема.
type RuleSetMatcher func(path, format, domain string) ([]int, error)

// Статусы шага проверки.
const (
	CheckMatch   = "match"   // правило сработало — проверка на нём и заканчивается
	CheckMiss    = "miss"    // правило проверено, не подошло
	CheckSkip    = "skip"    // по одному домену судить нельзя (IP, порт, процесс…)
	CheckUnknown = "unknown" // набор проверить нечем (удалённый ещё не скачан)
)

// CheckStep — результат проверки одного правила.
type CheckStep struct {
	Index  int    `json:"index"` // позиция во включённом списке
	RuleID string `json:"ruleId"`
	Title  string `json:"title"`
	Match  string `json:"match"`
	Action string `json:"action"`
	Target string `json:"target"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// DomainCheck — итог проверки домена по текущим правилам.
type DomainCheck struct {
	Domain string `json:"domain"`
	// Action — куда уйдёт трафик: proxy | direct | block.
	Action string `json:"action"`
	Target string `json:"target"` // группа нод, если правило её задаёт
	// RuleID/RuleTitle — сработавшее правило; пусто, если решение принял final.
	RuleID    string `json:"ruleId"`
	RuleTitle string `json:"ruleTitle"`
	ByFinal   bool   `json:"byFinal"` // решение принято правилом «всё остальное»
	// Mode — режим Clash API, при котором считали (Global/Direct перекрывают всё).
	Mode  string      `json:"mode"`
	Steps []CheckStep `json:"steps"`
}

// CheckDomain прогоняет домен по включённым правилам сверху вниз и говорит,
// какое из них сработает. Это статическая проверка: у неё нет ни IP назначения,
// ни процесса-инициатора, поэтому правила по IP/порту/процессу помечаются как
// пропущенные — вживую они могут сработать раньше найденного.
func CheckDomain(opts Options, domain string, match RuleSetMatcher) (DomainCheck, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		return DomainCheck{}, fmt.Errorf("пустой домен")
	}
	if i := strings.Index(domain, "://"); i >= 0 { // вставили ссылку целиком
		domain = domain[i+3:]
	}
	domain = strings.TrimSuffix(strings.SplitN(strings.SplitN(domain, "/", 2)[0], ":", 2)[0], ".")
	if domain == "" {
		return DomainCheck{}, fmt.Errorf("не удалось выделить домен")
	}

	mode := opts.Mode
	if mode != ModeGlobal && mode != ModeDirect {
		mode = ModeRule
	}
	out := DomainCheck{Domain: domain, Mode: mode}

	// Режимы Global/Direct стоят в конфиге выше пользовательских правил и
	// перекрывают их целиком — проверять список бессмысленно.
	switch mode {
	case ModeGlobal:
		out.Action, out.ByFinal = rules.ActionProxy, true
		return out, nil
	case ModeDirect:
		out.Action, out.ByFinal = rules.ActionDirect, true
		return out, nil
	}

	enabled := opts.Routing.EnabledRules()

	// Все доменные условия уходят в ядро одним временным набором: так семантика
	// domain_suffix/keyword/regex ровно та же, что в бою, а не наша догадка.
	hits, err := matchDomainRules(opts, enabled, domain, match)
	if err != nil {
		return DomainCheck{}, err
	}

	for i, r := range enabled {
		step := CheckStep{
			Index: i, RuleID: r.ID, Title: r.Title(),
			Match: r.Match, Action: r.Action, Target: r.Target,
		}
		switch r.Match {
		case rules.MatchDomain, rules.MatchDomainSuffix, rules.MatchDomainKeyword, rules.MatchDomainRegex:
			if hits[i] {
				step.Status = CheckMatch
			} else {
				step.Status = CheckMiss
			}
		case rules.MatchRuleSet:
			step.Status, step.Reason = checkRuleSet(opts, r, domain, match)
		case rules.MatchIPCIDR, rules.MatchPrivate:
			step.Status, step.Reason = CheckSkip, "правило по IP: сработает или нет — зависит от адреса, в который разрешится домен"
		case rules.MatchPort:
			step.Status, step.Reason = CheckSkip, "правило по порту: зависит от порта соединения"
		case rules.MatchProcess, rules.MatchProcessPath:
			step.Status, step.Reason = CheckSkip, "правило по процессу: зависит от того, какая программа откроет соединение"
		default:
			step.Status, step.Reason = CheckSkip, "правило по свойствам соединения, домена ему мало"
		}

		out.Steps = append(out.Steps, step)
		if step.Status == CheckMatch {
			out.Action, out.Target = r.Action, r.Target
			out.RuleID, out.RuleTitle = r.ID, r.Title()
			return out, nil
		}
	}

	out.ByFinal = true
	out.Action = opts.Routing.Final
	if out.Action == "" {
		out.Action = rules.ActionProxy
	}
	return out, nil
}

// matchDomainRules собирает доменные условия включённых правил в один
// source-набор и отдаёт ядру: ключ карты — индекс правила в enabled.
func matchDomainRules(opts Options, enabled []rules.Rule, domain string, match RuleSetMatcher) (map[int]bool, error) {
	type probeRule map[string][]string
	var probe []probeRule
	var ruleIdx []int // позиция в probe → индекс в enabled

	for i, r := range enabled {
		field, ok := matchFields[r.Match]
		if !ok || !strings.HasPrefix(r.Match, "domain") {
			continue
		}
		vals := rules.CleanValues(r.Match, r.Values)
		if len(vals) == 0 {
			continue
		}
		probe = append(probe, probeRule{field: vals})
		ruleIdx = append(ruleIdx, i)
	}
	hits := map[int]bool{}
	if len(probe) == 0 {
		return hits, nil
	}

	body, err := json.Marshal(map[string]interface{}{"version": 3, "rules": probe})
	if err != nil {
		return nil, err
	}
	// Временный набор кладём во временный каталог, а не рядом с ассетами:
	// портативная сборка вполне может лежать в каталоге без права записи.
	f, err := os.CreateTemp("", "domaincheck-*.json")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(body); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	matched, err := match(path, rules.FormatSource, domain)
	if err != nil {
		return nil, err
	}
	for _, n := range matched {
		if n >= 0 && n < len(ruleIdx) {
			hits[ruleIdx[n]] = true
		}
	}
	return hits, nil
}

// checkRuleSet проверяет правило-набор: локальные .srs спрашиваем у ядра,
// удалённые честно помечаем непроверяемыми — их содержимое лежит в кэше ядра.
func checkRuleSet(opts Options, r rules.Rule, domain string, match RuleSetMatcher) (string, string) {
	var unknown []string
	for _, tag := range rules.CleanValues(r.Match, r.Values) {
		if set := opts.Routing.FindRuleSet(tag); set != nil && set.Type == rules.SetRemote {
			unknown = append(unknown, tag)
			continue
		}
		if opts.RuleSetDir == "" {
			unknown = append(unknown, tag)
			continue
		}
		path := filepath.Join(opts.RuleSetDir, tag+".srs")
		if _, err := os.Stat(path); err != nil {
			continue // файла нет — в бою такое правило вообще не попадёт в конфиг
		}
		got, err := match(path, rules.FormatBinary, domain)
		if err != nil {
			unknown = append(unknown, tag)
			continue
		}
		if len(got) > 0 {
			return CheckMatch, ""
		}
	}
	if len(unknown) > 0 {
		return CheckUnknown, "удалённый набор " + strings.Join(unknown, ", ") + " проверяется только на живом ядре"
	}
	return CheckMiss, ""
}
