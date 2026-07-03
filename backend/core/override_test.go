//go:build coretest

// Тест механизма альтернативного ядра (вариант «официальное по умолчанию +
// опция своего sing-box»). Пути остаются встроенными, но бинарь переопределён.
// Запуск:
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"      # встроенное ядро/базы
//	$env:PROXY_FORK_CORE="C:\...\sing-box.exe"           # форк с with_xhttp
//	$env:PROXY_TEST_LINK="vless://...type=xhttp..."
//	go test -tags coretest ./backend/core -run TestCoreOverrideRunsXHTTP -v
package core_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
)

func TestCoreOverrideRunsXHTTP(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	fork := os.Getenv("PROXY_FORK_CORE")
	link := os.Getenv("PROXY_TEST_LINK")
	if assets == "" || fork == "" || link == "" {
		t.Skip("PROXY_ASSETS/PROXY_FORK_CORE/PROXY_TEST_LINK не заданы — пропускаем")
	}

	node, err := config.ParseLink(link)
	if err != nil {
		t.Fatalf("разбор ссылки: %v", err)
	}

	dataDir := t.TempDir()
	// Пути указывают на ВСТРОЕННОЕ ядро — как в обычном приложении.
	paths := &core.Paths{
		AssetsDir:  assets,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assets, "sing-box.exe"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}

	m := core.NewManager(paths)
	m.SetBinaryPath(fork) // переключаем на альтернативное ядро (форк)

	if got := m.BinaryPath(); got != fork {
		t.Fatalf("BinaryPath() = %q, ожидался форк %q", got, fork)
	}

	cfg, err := config.Generate(config.Options{
		MixedPort: 2080, ClashAPIPort: 9090, ClashSecret: "x",
		LogLevel: "info", CacheDBPath: "cache.db",
		Nodes: []config.Node{node},
	})
	if err != nil {
		t.Fatalf("генерация конфига: %v", err)
	}
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() { _ = m.Stop(); time.Sleep(500 * time.Millisecond) }()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не запустилось; лог:\n%s", dumpLogs(m))
	}

	directIP := fetchIP(directClient(), "")
	var proxyIP string
	deadline := time.Now().Add(25 * time.Second)
	pc := proxyClient(t, "http://127.0.0.1:2080")
	for time.Now().Before(deadline) {
		if ip := fetchIP(pc, ""); ip != "" {
			proxyIP = ip
			break
		}
		time.Sleep(time.Second)
	}
	if proxyIP == "" {
		t.Fatalf("нет IP через прокси; лог:\n%s", dumpLogs(m))
	}
	if directIP != "" && proxyIP == directIP {
		t.Fatalf("IP не сменился (%s) — трафик не заворачивается", proxyIP)
	}
	t.Logf("✅ альтернативное ядро (форк) поднято через override, xhttp работает: %s (напрямую %s)", proxyIP, orNA(directIP))
}
