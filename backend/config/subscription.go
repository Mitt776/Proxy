package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ValidateSubscriptionURL отвергает адрес подписки без TLS.
//
// Подписка — это точка, из которой приложение узнаёт, куда отправлять весь
// трафик пользователя. По http её содержимое подменяет кто угодно на пути
// (в первую очередь тот самый провайдер, от которого и защищаемся): в ответ
// приезжает своя нода, при желании со `skip-cert-verify`, и трафик едет через
// чужой сервер, а интерфейс показывает штатное «подключено». Проверять
// сертификаты нод бессмысленно, если сам список нод приехал открытым текстом.
//
// Схема сверяется до запроса, потому что перехваченный ответ уже поздно
// разбирать.
func ValidateSubscriptionURL(subURL string) error {
	u, err := url.Parse(strings.TrimSpace(subURL))
	if err != nil {
		return fmt.Errorf("адрес подписки не разобрался: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" {
			return fmt.Errorf("в адресе подписки нет хоста")
		}
		return nil
	case "http":
		return fmt.Errorf("подписка по http небезопасна — содержимое подменяется по пути, нужен https")
	case "":
		return fmt.Errorf("нужен https-адрес подписки, получено %q", subURL)
	default:
		return fmt.Errorf("подписка поддерживает только https, получено %q", u.Scheme)
	}
}

// FetchSubscription скачивает тело подписки по URL.
func FetchSubscription(ctx context.Context, subURL string) ([]byte, error) {
	// Проверка стоит здесь, а не только у вызывающих: через эту функцию идут и
	// добавление профиля, и ручное обновление, и тик планировщика подписок, —
	// а забыть её в одном из трёх мест ничего не мешает.
	if err := ValidateSubscriptionURL(subURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", subURL, nil)
	if err != nil {
		return nil, err
	}
	// Многие панели отдают base64/clash в зависимости от User-Agent; ставим нейтральный.
	req.Header.Set("User-Agent", "sing-box")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("загрузка подписки: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("подписка вернула статус %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // до 8 МБ
}

// DecodeSubscription определяет формат содержимого подписки и извлекает ноды.
// Поддержка: sing-box JSON, Clash YAML, base64-список ссылок, plain-текст со ссылками.
func DecodeSubscription(data []byte) ([]Node, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("пустая подписка")
	}

	// 1) sing-box JSON (объект с outbounds или массив outbound-ов).
	if trimmed[0] == '{' || trimmed[0] == '[' {
		if nodes, err := nodesFromJSON([]byte(trimmed)); err == nil && len(nodes) > 0 {
			return nodes, nil
		}
	}

	// 2) Clash YAML.
	if looksLikeClash(trimmed) {
		if nodes, err := parseClashYAML([]byte(trimmed)); err == nil && len(nodes) > 0 {
			return nodes, nil
		}
	}

	// 3) base64-список ссылок.
	if decoded, err := decodeBase64(trimmed); err == nil {
		if s := strings.TrimSpace(string(decoded)); strings.Contains(s, "://") {
			if nodes, _ := ParseLinks(s); len(nodes) > 0 {
				return nodes, nil
			}
		}
	}

	// 4) plain-текст со ссылками (по одной на строку).
	if strings.Contains(trimmed, "://") {
		nodes, errs := ParseLinks(trimmed)
		if len(nodes) > 0 {
			return nodes, nil
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("не удалось разобрать ссылки: %v", errs[0])
		}
	}

	return nil, fmt.Errorf("неизвестный формат подписки")
}

// nodesFromJSON извлекает ноды из sing-box JSON (полный config или массив outbound-ов).
func nodesFromJSON(data []byte) ([]Node, error) {
	trimmed := strings.TrimSpace(string(data))
	var arr []json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}
	} else {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, err
		}
		outs, ok := obj["outbounds"]
		if !ok {
			// одиночный outbound-объект
			return ExtractNodes([]json.RawMessage{data})
		}
		if err := json.Unmarshal(outs, &arr); err != nil {
			return nil, err
		}
	}
	return ExtractNodes(arr)
}

// ExtractNodes отбрасывает служебные outbound-ы (direct/block/selector/urltest/dns),
// оставляя реальные ноды-серверы. Экспортируется для переиспользования (напр. в app).
func ExtractNodes(arr []json.RawMessage) ([]Node, error) {
	skip := map[string]bool{
		"direct": true, "block": true, "dns": true,
		"selector": true, "urltest": true,
	}
	var nodes []Node
	for _, raw := range arr {
		var meta struct {
			Type string `json:"type"`
			Tag  string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("некорректный outbound: %w", err)
		}
		if meta.Type == "" || skip[meta.Type] {
			continue
		}
		nodes = append(nodes, Node{Tag: meta.Tag, Outbound: raw})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("не найдено ни одной ноды-сервера")
	}
	return nodes, nil
}

func looksLikeClash(s string) bool {
	return strings.Contains(s, "proxies:") &&
		(strings.Contains(s, "\n- ") || strings.Contains(s, "\n  - ") || strings.Contains(s, "- {"))
}
