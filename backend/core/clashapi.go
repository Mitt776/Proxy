package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ClashClient — тонкий клиент Clash API ядра sing-box (external_controller).
// Через него UI получает список нод, тестирует задержку, переключает selector
// и опрашивает трафик/соединения. Всё поверх stdlib net/http.
type ClashClient struct {
	base   string // http://127.0.0.1:9090
	secret string
	hc     *http.Client
}

// NewClashClient создаёт клиент для адреса вида "127.0.0.1:9090" и секрета.
func NewClashClient(addr, secret string) *ClashClient {
	return &ClashClient{
		base:   "http://" + addr,
		secret: secret,
		hc:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Proxy — одна запись из /proxies (нода, selector или urltest).
type Proxy struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`    // Selector|URLTest|Vless|Trojan|...
	Now     string         `json:"now"`     // выбранная нода (для Selector/URLTest)
	All     []string       `json:"all"`     // варианты (для Selector/URLTest)
	History []delayHistory `json:"history"` // последние замеры задержки
	Udp     bool           `json:"udp"`
}

type delayHistory struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"` // мс; 0 — недоступно
}

// LastDelay возвращает последнюю измеренную задержку ноды (0 — нет данных).
func (p Proxy) LastDelay() int {
	if len(p.History) == 0 {
		return 0
	}
	return p.History[len(p.History)-1].Delay
}

// Proxies возвращает карту всех прокси по имени.
func (c *ClashClient) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var out struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.getJSON(ctx, "/proxies", &out); err != nil {
		return nil, err
	}
	return out.Proxies, nil
}

// SelectProxy переключает selector на конкретную ноду (PUT /proxies/{selector}).
func (c *ClashClient) SelectProxy(ctx context.Context, selector, name string) error {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, err := c.newRequest(ctx, http.MethodPut, "/proxies/"+url.PathEscape(selector), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clash api select: статус %d", resp.StatusCode)
	}
	return nil
}

// Delay замеряет задержку ноды через ядро (GET /proxies/{name}/delay).
// Возвращает задержку в мс. timeout — верхняя граница ожидания в мс.
func (c *ClashClient) Delay(ctx context.Context, name, testURL string, timeoutMS int) (int, error) {
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	if timeoutMS == 0 {
		timeoutMS = 5000
	}
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", strconv.Itoa(timeoutMS))
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + q.Encode()

	var out struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return 0, err
	}
	if out.Message != "" && out.Delay == 0 {
		return 0, fmt.Errorf("%s", out.Message)
	}
	return out.Delay, nil
}

// ConnMetadata — описание соединения, которое ядро отдаёт в /connections.
type ConnMetadata struct {
	Network         string `json:"network"` // tcp|udp
	Type            string `json:"type"`    // тип inbound-а (mixed, tun)
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"` // домен из sniff-а; пусто — соединение по IP
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

// Connection — одно активное соединение ядра.
type Connection struct {
	ID       string       `json:"id"`
	Metadata ConnMetadata `json:"metadata"`
	Upload   int64        `json:"upload"`
	Download int64        `json:"download"`
	Start    time.Time    `json:"start"`
	// Chains — цепочка outbound-ов, последний элемент — куда трафик ушёл в итоге.
	Chains []string `json:"chains"`
	// Rule/RulePayload — сработавшее route-правило ядра ("rule_set", "geosite-ru").
	Rule        string `json:"rule"`
	RulePayload string `json:"rulePayload"`
}

// Traffic — суммарные счётчики и активные соединения из /connections.
type Traffic struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
}

// Connections возвращает суммарные байты и число активных соединений.
func (c *ClashClient) Connections(ctx context.Context) (Traffic, error) {
	var t Traffic
	err := c.getJSON(ctx, "/connections", &t)
	return t, err
}

// CloseConnection обрывает одно соединение (DELETE /connections/{id}).
func (c *ClashClient) CloseConnection(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil)
}

// CloseAllConnections обрывает все активные соединения. Полезно после смены
// правил: уже установленные соединения продолжают идти по старому маршруту,
// пока их не разорвать.
func (c *ClashClient) CloseAllConnections(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil)
}

// Mode возвращает текущий режим маршрутизации ядра (Rule|Global|Direct).
func (c *ClashClient) Mode(ctx context.Context) (string, error) {
	var out struct {
		Mode string `json:"mode"`
	}
	if err := c.getJSON(ctx, "/configs", &out); err != nil {
		return "", err
	}
	return out.Mode, nil
}

// SetMode переключает режим на лету (PATCH /configs) — без перезапуска ядра
// и, что важнее, без разрыва активных соединений и системного прокси.
func (c *ClashClient) SetMode(ctx context.Context, mode string) error {
	body, _ := json.Marshal(map[string]string{"mode": mode})
	req, err := c.newRequest(ctx, http.MethodPatch, "/configs", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clash api mode: статус %d", resp.StatusCode)
	}
	return nil
}

// Version проверяет, что Clash API отвечает (используется как ping готовности).
func (c *ClashClient) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(ctx, "/version", &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// --- низкоуровневое ---

func (c *ClashClient) getJSON(ctx context.Context, path string, dst interface{}) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clash api %s: статус %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *ClashClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		r.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return r, nil
}

// do выполняет запрос без разбора ответа и превращает не-2xx в ошибку.
func (c *ClashClient) do(ctx context.Context, method, path string, body io.Reader) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clash api %s: статус %d", path, resp.StatusCode)
	}
	return nil
}
