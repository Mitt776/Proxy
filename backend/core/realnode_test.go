//go:build coretest

// Сквозной тест реального перехвата через ноду из ссылки.
// Запуск (из корня проекта):
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	$env:PROXY_TEST_LINK="vless://...."
//	go test -tags coretest ./backend/core -run TestRealNodeProxying -v
package core_test

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
)

func TestRealNodeProxying(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	link := os.Getenv("PROXY_TEST_LINK")
	if assets == "" || link == "" {
		t.Skip("PROXY_ASSETS или PROXY_TEST_LINK не заданы — пропускаем сквозной тест")
	}

	node, err := config.ParseLink(link)
	if err != nil {
		t.Fatalf("разбор ссылки: %v", err)
	}
	t.Logf("нода распознана: tag=%q", node.Tag)

	dataDir := t.TempDir()
	paths := &core.Paths{
		AssetsDir:  assets,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assets, "sing-box.exe"),
		GeoIP:      filepath.Join(assets, "geoip.db"),
		GeoSite:    filepath.Join(assets, "geosite.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}

	cfg, err := config.Generate(config.Options{
		MixedPort:    2080,
		ClashAPIPort: 9090,
		ClashSecret:  "testsecret",
		LogLevel:     "info",
		CacheDBPath:  "cache.db",
		Nodes:        []config.Node{node},
	})
	if err != nil {
		t.Fatalf("генерация конфига: %v", err)
	}

	m := core.NewManager(paths)
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() {
		_ = m.Stop()
		time.Sleep(500 * time.Millisecond)
	}()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не запустилось; лог:\n%s", dumpLogs(m))
	}

	// Прямой внешний IP (без прокси).
	directIP := fetchIP(directClient(), "")
	t.Logf("IP напрямую: %s", orNA(directIP))

	// Проксированный внешний IP (через mixed HTTP-proxy 127.0.0.1:2080).
	// Reality-хендшейк может занять время — поллим до 25с.
	var proxyIP string
	deadline := time.Now().Add(25 * time.Second)
	pc := proxyClient(t, "http://127.0.0.1:2080")
	for time.Now().Before(deadline) {
		if ip := fetchIP(pc, ""); ip != "" {
			proxyIP = ip
			break
		}
		time.Sleep(1 * time.Second)
	}

	if proxyIP == "" {
		t.Fatalf("не удалось получить IP через прокси; лог ядра:\n%s", dumpLogs(m))
	}
	t.Logf("IP через прокси: %s", proxyIP)

	if directIP != "" && proxyIP == directIP {
		t.Fatalf("IP через прокси совпал с прямым (%s) — трафик НЕ заворачивается", proxyIP)
	}
	t.Logf("✅ перехват работает: трафик выходит через ноду (%s), а не напрямую (%s)", proxyIP, orNA(directIP))
}

func directClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second}
}

func proxyClient(t *testing.T, proxyURL string) *http.Client {
	pu, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("proxy url: %v", err)
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(pu)},
	}
}

// fetchIP запрашивает внешний IP через несколько сервисов (plain HTTP,
// чтобы mixed HTTP-proxy мог их проксировать без CONNECT/TLS).
func fetchIP(c *http.Client, _ string) string {
	endpoints := []string{
		"http://api.ipify.org/",
		"http://ifconfig.me/ip",
		"http://icanhazip.com/",
	}
	for _, ep := range endpoints {
		resp, err := c.Get(ep)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if resp.StatusCode == 200 && looksLikeIP(ip) {
			return ip
		}
	}
	return ""
}

func looksLikeIP(s string) bool {
	if s == "" || len(s) > 45 {
		return false
	}
	return strings.Count(s, ".") == 3 || strings.Contains(s, ":")
}

func orNA(s string) string {
	if s == "" {
		return "н/д"
	}
	return s
}
