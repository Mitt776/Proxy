//go:build coretest

// Проверка двух механизмов 1.2.0 на живом ядре:
//   - Manager.Restart перезапускает ядро, не показывая наверх «остановлено»
//     (иначе app.go снял бы системный прокси у активного соединения);
//   - режим Clash API переключается через PATCH /configs без перезапуска.
//
// Запуск:
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	go test -tags coretest ./backend/core -run "TestRestartKeepsConnectionAlive|TestModeSwitchLive" -v
package core_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
	"Proxy/backend/rules"
)

// testPaths собирает Paths поверх реальных ассетов и временного каталога данных.
func testPaths(t *testing.T) *core.Paths {
	t.Helper()
	assets := os.Getenv("PROXY_ASSETS")
	if assets == "" {
		t.Skip("PROXY_ASSETS не задан — пропускаем интеграционный тест")
	}
	dataDir := t.TempDir()
	return &core.Paths{
		AssetsDir:  assets,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assets, "sing-box.exe"),
		Wintun:     filepath.Join(assets, "wintun.dll"),
		GeoIP:      filepath.Join(assets, "geoip.db"),
		GeoSite:    filepath.Join(assets, "geosite.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}
}

// testConfig — конфиг без нод (весь трафик direct), но с правилами и режимом:
// для проверки перезапуска и Clash API реальный сервер не нужен.
func testConfig(t *testing.T, secret, mode string, blockedDomain string) []byte {
	t.Helper()
	cfg, err := config.Generate(config.Options{
		MixedPort:    2080,
		ClashAPIPort: 9090,
		ClashSecret:  secret,
		LogLevel:     "info",
		CacheDBPath:  "cache.db",
		Mode:         mode,
		Routing: rules.Config{Version: rules.Version, Final: rules.ActionDirect,
			Rules: []rules.Rule{{Enabled: true, Match: rules.MatchDomainSuffix,
				Values: []string{blockedDomain}, Action: rules.ActionBlock}}},
	})
	if err != nil {
		t.Fatalf("генерация конфига: %v", err)
	}
	return cfg
}

func TestRestartKeepsConnectionAlive(t *testing.T) {
	paths := testPaths(t)
	const secret = "restartsecret"

	m := core.NewManager(paths)

	// Собираем все состояния, которые менеджер отдаёт наверх.
	var mu sync.Mutex
	var states []core.State
	m.OnState = func(s core.State, _ string) {
		mu.Lock()
		states = append(states, s)
		mu.Unlock()
	}

	cfg := testConfig(t, secret, config.ModeRule, "first.example")
	if err := m.Check(cfg); err != nil {
		t.Fatalf("Check отверг валидный конфиг: %v", err)
	}
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() {
		_ = m.Stop()
		waitState(m, core.StateStopped, 5*time.Second)
	}()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не перешло в running; лог:\n%s", dumpLogs(m))
	}
	if err := waitClashAPI(secret, 8*time.Second); err != nil {
		t.Fatalf("Clash API недоступен: %v", err)
	}

	// Забываем всё, что было до перезапуска: интересны только события после.
	mu.Lock()
	states = nil
	mu.Unlock()

	next := testConfig(t, secret, config.ModeRule, "second.example")
	if err := m.Restart(next); err != nil {
		t.Fatalf("перезапуск: %v", err)
	}
	if !waitState(m, core.StateRunning, 8*time.Second) {
		t.Fatalf("ядро не поднялось после перезапуска; лог:\n%s", dumpLogs(m))
	}
	if err := waitClashAPI(secret, 8*time.Second); err != nil {
		t.Fatalf("после перезапуска Clash API недоступен: %v", err)
	}

	// Главная проверка: «остановлено» наверх не ушло. Иначе app.go снял бы
	// системный прокси и пользователь на секунду остался бы без интернета.
	mu.Lock()
	got := append([]core.State(nil), states...)
	mu.Unlock()
	for _, s := range got {
		if s == core.StateStopped || s == core.StateError {
			t.Fatalf("при перезапуске наверх ушло состояние %q (последовательность: %v)", s, got)
		}
	}
	t.Logf("✅ перезапуск прошёл без события «остановлено» (события: %v)", got)

	// Новый конфиг действительно применился — ядро читает config.json с диска.
	written, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(written, "second.example") {
		t.Fatal("после перезапуска на диске остался старый config.json")
	}
}

func TestModeSwitchLive(t *testing.T) {
	paths := testPaths(t)
	const secret = "modesecret"

	m := core.NewManager(paths)
	cfg := testConfig(t, secret, config.ModeGlobal, "blocked.example")
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() {
		_ = m.Stop()
		waitState(m, core.StateStopped, 5*time.Second)
	}()

	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не перешло в running; лог:\n%s", dumpLogs(m))
	}
	if err := waitClashAPI(secret, 8*time.Second); err != nil {
		t.Fatalf("Clash API недоступен: %v", err)
	}

	c := core.NewClashClient("127.0.0.1:9090", secret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// default_mode из конфига должен доехать до ядра.
	mode, err := c.Mode(ctx)
	if err != nil {
		t.Fatalf("чтение режима: %v", err)
	}
	if !equalFold(mode, config.ModeGlobal) {
		t.Fatalf("стартовый режим %q, ожидался %q", mode, config.ModeGlobal)
	}

	// И переключаться на лету, без перезапуска процесса.
	for _, want := range []string{config.ModeDirect, config.ModeRule, config.ModeGlobal} {
		if err := c.SetMode(ctx, want); err != nil {
			t.Fatalf("установка режима %q: %v", want, err)
		}
		got, err := c.Mode(ctx)
		if err != nil {
			t.Fatalf("чтение режима: %v", err)
		}
		if !equalFold(got, want) {
			t.Fatalf("режим %q, ожидался %q", got, want)
		}
		if m.State() != core.StateRunning {
			t.Fatalf("смена режима перезапустила ядро (состояние %q)", m.State())
		}
	}
	t.Log("✅ режим переключается через Clash API без перезапуска ядра")
}

func contains(hay []byte, needle string) bool {
	return len(needle) > 0 && len(hay) >= len(needle) && indexOf(hay, needle) >= 0
}

func indexOf(hay []byte, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) == needle {
			return i
		}
	}
	return -1
}

// equalFold сравнивает режимы без учёта регистра: ядро может вернуть "global"
// там, где мы отправили "Global".
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
