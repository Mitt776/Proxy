package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"Proxy/backend/rules"
)

// Поддержка текстовых списков доменов (*.lst) как наборов правил.
//
// Ядро такой формат не понимает: у sing-box есть только скомпилированный .srs и
// исходный JSON. Поэтому список качаем сами, переводим в source-набор и кладём
// в data\rulesets\<tag>.json — в конфиг он попадает уже локальным набором.
// Побочная выгода: содержимое лежит у нас на диске, значит «Проверка домена»
// умеет спрашивать по нему ядро, чего с настоящими remote-наборами не выйдет.

// ListSetPath возвращает путь к сконвертированному набору.
func ListSetPath(dir, tag string) string {
	return filepath.Join(dir, tag+".json")
}

// headlessRule — одно правило внутри source-набора sing-box.
type headlessRule struct {
	Domain       []string `json:"domain,omitempty"`
	DomainSuffix []string `json:"domain_suffix,omitempty"`
}

type sourceRuleSet struct {
	Version int            `json:"version"`
	Rules   []headlessRule `json:"rules"`
}

// ParseDomainList разбирает текстовый список доменов.
//
// Возвращает два набора: точные имена и суффиксы. Голый `example.com` даёт и то
// и другое (`example.com` + `.example.com`) — иначе пришлось бы выбирать между
// «не ловим поддомены» и суффиксом без точки, который совпал бы ещё и с
// `notexample.com`: domain_suffix у sing-box — обычное сравнение хвоста строки.
func ParseDomainList(data []byte) (domains, suffixes []string) {
	seenD := map[string]bool{}
	seenS := map[string]bool{}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		// Комментарий в хвосте строки.
		if i := strings.IndexAny(line, "#!"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		// Синтаксис фильтров: ||example.com^ и *.example.com. Звёздочка означает
		// только поддомены, поэтому превращаем её в ведущую точку, а не в голое
		// имя: иначе список «*.example.com» начал бы ловить и сам example.com.
		line = strings.TrimPrefix(line, "||")
		line = strings.TrimSuffix(line, "^")
		if strings.HasPrefix(line, "*.") {
			line = line[1:]
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || line == "." {
			continue
		}
		// Не домен: путь, пробел, схема, голый IP-диапазон.
		if strings.ContainsAny(line, " \t/:,") {
			continue
		}

		if strings.HasPrefix(line, ".") {
			if !seenS[line] {
				seenS[line] = true
				suffixes = append(suffixes, line)
			}
			continue
		}
		if !strings.Contains(line, ".") {
			continue // одиночная метка вроде "localhost" — не список доменов
		}
		if !seenD[line] {
			seenD[line] = true
			domains = append(domains, line)
		}
		if s := "." + line; !seenS[s] {
			seenS[s] = true
			suffixes = append(suffixes, s)
		}
	}
	return domains, suffixes
}

// BuildSourceRuleSet собирает JSON source-набора из списков.
//
// Точные имена и суффиксы идут **разными правилами**: поля внутри одного
// правила ядро объединяет по «и», и один объект с обоими полями требовал бы
// совпадения сразу по двум условиям.
func BuildSourceRuleSet(domains, suffixes []string) ([]byte, error) {
	set := sourceRuleSet{Version: 3}
	if len(domains) > 0 {
		set.Rules = append(set.Rules, headlessRule{Domain: domains})
	}
	if len(suffixes) > 0 {
		set.Rules = append(set.Rules, headlessRule{DomainSuffix: suffixes})
	}
	if len(set.Rules) == 0 {
		return nil, fmt.Errorf("в списке нет ни одного домена")
	}
	return json.Marshal(set)
}

// FetchList скачивает текстовый список. client позволяет вызывающему подсунуть
// клиент через прокси — для источников, заблокированных у провайдера.
func FetchList(ctx context.Context, listURL string, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sing-box")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("загрузка списка: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("список вернул статус %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // до 16 МБ
}

// SyncListSet качает список и перезаписывает сконвертированный набор на диске.
// Возвращает число доменов в наборе.
//
// Запись атомарная: ядро может читать файл прямо сейчас, а половинчатый JSON
// уронил бы его на следующем перезапуске.
func SyncListSet(ctx context.Context, rs rules.RuleSet, dir string, client *http.Client) (int, error) {
	body, err := FetchList(ctx, rs.URL, client)
	if err != nil {
		return 0, err
	}
	domains, suffixes := ParseDomainList(body)
	data, err := BuildSourceRuleSet(domains, suffixes)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	path := ListSetPath(dir, rs.Tag)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return len(domains) + len(suffixes), nil
}

// RemoveListSet убирает сконвертированный набор — вызывается при удалении
// описания, иначе в data\rulesets копятся файлы от давно забытых списков.
func RemoveListSet(dir, tag string) {
	if dir == "" || tag == "" {
		return
	}
	os.Remove(ListSetPath(dir, tag))
}
