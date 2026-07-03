//go:build coretest

// Сквозной тест клиента Clash API против реального ядра на живой ноде.
// Запуск (из корня проекта):
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	$env:PROXY_TEST_LINK="vless://...."
//	go test -tags coretest ./backend/core -run TestClashAPIAgainstCore -v
package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
)

func TestClashAPIAgainstCore(t *testing.T) {
	assets := os.Getenv("PROXY_ASSETS")
	link := os.Getenv("PROXY_TEST_LINK")
	if assets == "" || link == "" {
		t.Skip("PROXY_ASSETS или PROXY_TEST_LINK не заданы — пропускаем")
	}

	node, err := config.ParseLink(link)
	if err != nil {
		t.Fatalf("разбор ссылки: %v", err)
	}

	dataDir := t.TempDir()
	paths := &core.Paths{
		AssetsDir:  assets,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assets, "sing-box.exe"),
		GeoIP:      filepath.Join(assets, "geoip.db"),
		GeoSite:    filepath.Join(assets, "geosite.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}

	// ru-direct + блок рекламы: проверяем, что rule_set (.srs) реально грузятся ядром.
	cfg, err := config.Generate(config.Options{
		MixedPort:    2080,
		ClashAPIPort: 9090,
		ClashSecret:  "testsecret",
		LogLevel:     "info",
		CacheDBPath:  "cache.db",
		Nodes:        []config.Node{node},
		RoutingMode:  config.RoutingRUDirect,
		BlockAds:     true,
		RuleSetDir:   assets,
	})
	if err != nil {
		t.Fatalf("генерация конфига: %v", err)
	}

	m := core.NewManager(paths)
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() { _ = m.Stop(); time.Sleep(500 * time.Millisecond) }()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не запустилось (rule_set не загрузился?); лог:\n%s", dumpLogs(m))
	}

	cc := core.NewClashClient("127.0.0.1:9090", "testsecret")
	ctx := context.Background()

	// Ждём готовности Clash API (поднимается через долю секунды после старта).
	var ready bool
	for i := 0; i < 20; i++ {
		if _, err := cc.Version(ctx); err == nil {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("Clash API не ответил; лог:\n%s", dumpLogs(m))
	}

	// /proxies: должен быть selector "proxy" с "auto" и нашей нодой.
	proxies, err := cc.Proxies(ctx)
	if err != nil {
		t.Fatalf("Proxies: %v", err)
	}
	sel, ok := proxies[config.ProxyTag]
	if !ok {
		t.Fatalf("нет selector %q в /proxies", config.ProxyTag)
	}
	if len(sel.All) < 2 {
		t.Fatalf("selector.All = %v, ожидались минимум auto+нода", sel.All)
	}
	t.Logf("✅ /proxies: selector=%q now=%q варианты=%v", config.ProxyTag, sel.Now, sel.All)

	// Находим реальную ноду (не auto) для delay/select.
	var nodeName string
	for _, n := range sel.All {
		if n != config.AutoTag {
			nodeName = n
			break
		}
	}

	// /proxies/{node}/delay — задержка через ядро.
	delay, err := cc.Delay(ctx, nodeName, "", 5000)
	if err != nil {
		t.Logf("⚠ delay(%q) вернул ошибку (нода могла не ответить вовремя): %v", nodeName, err)
	} else {
		t.Logf("✅ delay(%q) = %d ms", nodeName, delay)
	}

	// Переключение selector на конкретную ноду.
	if err := cc.SelectProxy(ctx, config.ProxyTag, nodeName); err != nil {
		t.Fatalf("SelectProxy: %v", err)
	}
	proxies2, err := cc.Proxies(ctx)
	if err != nil {
		t.Fatalf("Proxies после select: %v", err)
	}
	if got := proxies2[config.ProxyTag].Now; got != nodeName {
		t.Fatalf("после select now=%q, ожидалось %q", got, nodeName)
	}
	t.Logf("✅ select: selector теперь указывает на %q", nodeName)

	// /connections — суммарные счётчики.
	tr, err := cc.Connections(ctx)
	if err != nil {
		t.Fatalf("Connections: %v", err)
	}
	t.Logf("✅ /connections: down=%d up=%d активных=%d", tr.DownloadTotal, tr.UploadTotal, len(tr.Connections))
}
