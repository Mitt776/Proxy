//go:build coretest

// Интеграционный тест связки с реальным sing-box.exe.
// Запуск (из корня проекта):
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	go test -tags coretest ./backend/core -run TestCoreEndToEnd -v
package core_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
)

func TestCoreEndToEnd(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан — пропускаем интеграционный тест")
	}

	dataDir := t.TempDir()
	paths := &core.Paths{
		AssetsDir:  assets,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assets, "sing-box.exe"),
		Wintun:     filepath.Join(assets, "wintun.dll"),
		GeoIP:      filepath.Join(assets, "geoip.db"),
		GeoSite:    filepath.Join(assets, "geosite.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}

	const secret = "testsecret"
	cfg, err := config.Generate(config.Options{
		MixedPort:    2080,
		ClashAPIPort: 9090,
		ClashSecret:  secret,
		LogLevel:     "info",
		CacheDBPath:  "cache.db",
		// без нод → route.final = direct, ядро поднимается для проверки портов
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
		waitState(m, core.StateStopped, 5*time.Second)
		if s := m.State(); s == core.StateRunning {
			t.Errorf("ядро не остановилось, состояние=%s", s)
		}
	}()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не перешло в running; лог:\n%s", dumpLogs(m))
	}

	// 1) Clash API отвечает.
	if err := waitClashAPI(secret, 8*time.Second); err != nil {
		t.Fatalf("Clash API недоступен: %v; лог:\n%s", err, dumpLogs(m))
	}
	t.Log("✅ Clash API отвечает на 127.0.0.1:9090")

	// 2) mixed inbound слушает.
	if err := waitTCP("127.0.0.1:2080", 5*time.Second); err != nil {
		t.Fatalf("mixed-порт не слушается: %v", err)
	}
	t.Log("✅ mixed inbound слушает 127.0.0.1:2080")
}

func waitState(m *core.Manager, want core.State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.State() == want {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return m.State() == want
}

func waitClashAPI(secret string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:9090/version", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			lastErr = fmt.Errorf("статус %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func waitTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}

func dumpLogs(m *core.Manager) string {
	out := ""
	for _, l := range m.Logs() {
		out += l + "\n"
	}
	return out
}
