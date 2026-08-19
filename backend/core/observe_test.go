//go:build coretest

// Наблюдаемость (1.3.0) на живом ядре:
//   - /connections отдаёт соединения с адресом, правилом и цепочкой outbound-ов;
//   - соединение можно оборвать через Clash API;
//   - `sing-box rule-set match` отвечает на вопрос «домен ∈ набор».
//
// Запуск:
//
//	$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"
//	go test -tags coretest ./backend/core -run "TestConnectionsLive|TestRuleSetMatchLive" -v
package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/core"
)

func TestConnectionsLive(t *testing.T) {
	paths := testPaths(t)
	const secret = "connsecret"

	m := core.NewManager(paths)
	cfg := testConfig(t, secret, config.ModeRule, "blocked.example")
	if err := m.Start(cfg); err != nil {
		t.Fatalf("старт ядра: %v", err)
	}
	defer func() {
		_ = m.Stop()
		waitState(m, core.StateStopped, 5*time.Second)
	}()
	if !waitState(m, core.StateRunning, 5*time.Second) {
		t.Fatalf("ядро не поднялось; лог:\n%s", dumpLogs(m))
	}
	if err := waitClashAPI(secret, 8*time.Second); err != nil {
		t.Fatalf("Clash API недоступен: %v", err)
	}

	// Локальный сервер вместо интернета: тест не должен зависеть от сети.
	// Держим соединение открытым, чтобы успеть увидеть его в /connections.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)

	proxyURL, _ := url.Parse("http://127.0.0.1:2080")
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 10 * time.Second}
	go func() { _, _ = hc.Get(srv.URL) }() // висит, пока не закроем release

	c := core.NewClashClient("127.0.0.1:9090", secret)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var found core.Connection
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		tr, err := c.Connections(ctx)
		if err == nil {
			for _, conn := range tr.Connections {
				if conn.Metadata.DestinationIP != "" || conn.Metadata.Host != "" {
					found = conn
					break
				}
			}
		}
		if found.ID != "" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if found.ID == "" {
		t.Fatalf("активных соединений не видно; лог ядра:\n%s", dumpLogs(m))
	}

	// Ровно те поля, на которых держится окно соединений.
	if found.Metadata.Network == "" {
		t.Error("нет network у соединения")
	}
	if found.Metadata.DestinationPort == "" {
		t.Error("нет порта назначения")
	}
	if len(found.Chains) == 0 {
		t.Error("нет цепочки outbound-ов — UI не покажет, куда ушёл трафик")
	}
	if found.Start.IsZero() {
		t.Error("нет времени старта — не посчитать возраст соединения")
	}
	t.Logf("✅ соединение: %s:%s network=%s chains=%v rule=%q payload=%q process=%q",
		found.Metadata.DestinationIP, found.Metadata.DestinationPort, found.Metadata.Network,
		found.Chains, found.Rule, found.RulePayload, found.Metadata.Process)

	if err := c.CloseConnection(ctx, found.ID); err != nil {
		t.Fatalf("обрыв соединения: %v", err)
	}
	gone := false
	for i := 0; i < 20 && !gone; i++ {
		tr, err := c.Connections(ctx)
		if err == nil {
			gone = true
			for _, conn := range tr.Connections {
				if conn.ID == found.ID {
					gone = false
				}
			}
		}
		if !gone {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if !gone {
		t.Fatal("соединение осталось в списке после DELETE /connections/{id}")
	}
	if err := c.CloseAllConnections(ctx); err != nil {
		t.Fatalf("обрыв всех соединений: %v", err)
	}
	t.Log("✅ соединения обрываются поштучно и целиком")
}

func TestRuleSetMatchLive(t *testing.T) {
	paths := testPaths(t)
	m := core.NewManager(paths)

	srs := filepath.Join(paths.AssetsDir, "geosite-ru.srs")
	if _, err := os.Stat(srs); err != nil {
		t.Skipf("нет %s", srs)
	}
	hit, err := m.RuleSetMatch(srs, "binary", "yandex.ru")
	if err != nil {
		t.Fatalf("match по .srs: %v", err)
	}
	if len(hit) == 0 {
		t.Fatal("yandex.ru не найден в geosite-ru — проверка домена работать не будет")
	}
	miss, err := m.RuleSetMatch(srs, "binary", "wikipedia.org")
	if err != nil {
		t.Fatalf("match по .srs: %v", err)
	}
	if len(miss) != 0 {
		t.Fatalf("wikipedia.org неожиданно попал в geosite-ru: %v", miss)
	}

	// Тот же путь, которым ходит проверка домена: временный source-набор,
	// индексы совпавших правил.
	probe := filepath.Join(t.TempDir(), "probe.json")
	body := `{"version":3,"rules":[{"domain_suffix":["nomatch.test"]},{"domain_suffix":["example.com"]}]}`
	if err := os.WriteFile(probe, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := m.RuleSetMatch(probe, "source", "www.example.com")
	if err != nil {
		t.Fatalf("match по source: %v", err)
	}
	if len(idx) != 1 || idx[0] != 1 {
		t.Fatalf("ожидался индекс [1], получено %v", idx)
	}
	t.Log("✅ ядро отвечает на rule-set match и по .srs, и по временному набору")
}
