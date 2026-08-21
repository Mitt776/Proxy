package appcore

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"Proxy/backend/core"
	"Proxy/backend/rules"
)

// --- заглушки платформы ---

type fakeHost struct {
	mu     sync.Mutex
	events []string
}

func (h *fakeHost) Emit(name string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, name)
}

func (h *fakeHost) Logf(string, ...any)  {}
func (h *fakeHost) ProfilesChanged()     {}
func (h *fakeHost) OnStats(int64, int64) {}
func (h *fakeHost) DefaultLang() string  { return "ru" }

func (h *fakeHost) sawEvent(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.events {
		if e == name {
			return true
		}
	}
	return false
}

// fakeRunner изображает ядро: считает вызовы и умеет отвергать конфиг, как это
// сделал бы настоящий `sing-box check`.
type fakeRunner struct {
	state      core.State
	checkErr   error
	checks     int
	restarts   int
	lastConfig []byte
}

func (r *fakeRunner) State() core.State { return r.state }
func (r *fakeRunner) Logs() []string    { return nil }
func (r *fakeRunner) Stop() error       { r.state = core.StateStopped; return nil }

func (r *fakeRunner) Check(configJSON []byte) error {
	r.checks++
	return r.checkErr
}

func (r *fakeRunner) Start(configJSON []byte) error {
	r.lastConfig = configJSON
	r.state = core.StateRunning
	return nil
}

func (r *fakeRunner) Restart(configJSON []byte) error {
	r.restarts++
	r.lastConfig = configJSON
	return nil
}

func newTestCore(t *testing.T) (*Core, *fakeHost, *fakeRunner) {
	t.Helper()
	dir := t.TempDir()
	host := &fakeHost{}
	c := New(Options{
		Host:  host,
		Paths: &core.Paths{DataDir: dir, AssetsDir: filepath.Join(dir, "assets")},
	})
	if issues := c.Load(); len(issues) > 0 {
		t.Fatalf("чистый каталог, а хранилища не прочитались: %v", issues)
	}
	runner := &fakeRunner{state: core.StateStopped}
	c.SetRunner(runner)
	t.Cleanup(c.Close)
	return c, host, runner
}

// TestRoutingRollbackOnCoreReject — центральный инвариант маршрутизации: если ядро
// отвергло конфиг, правило не должно остаться на диске. Иначе UI показывает одно,
// трафик идёт по-другому, и разобраться в этом пользователю нечем.
func TestRoutingRollbackOnCoreReject(t *testing.T) {
	c, _, runner := newTestCore(t)

	// Профиль нужен, чтобы ApplyRouting дошёл до генерации конфига.
	if _, err := c.AddManualProfile("тест", "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls&sni=example.com#node"); err != nil {
		t.Fatalf("профиль: %v", err)
	}
	runner.state = core.StateRunning

	before := len(c.GetRouting().Rules)
	runner.checkErr = errors.New("decode config: unknown field")

	_, err := c.AddRule(rules.Rule{
		Name: "тест", Enabled: true,
		Match: rules.MatchDomainSuffix, Values: []string{"example.com"},
		Action: rules.ActionDirect,
	})
	if err == nil {
		t.Fatal("ядро отвергло конфиг, а AddRule вернул успех")
	}
	if runner.checks == 0 {
		t.Fatal("конфиг не проверялся ядром до перезапуска")
	}
	if runner.restarts != 0 {
		t.Fatal("ядро перезапущено на конфиге, который само же отвергло")
	}
	if got := len(c.GetRouting().Rules); got != before {
		t.Fatalf("правило не откатилось: было %d правил, стало %d", before, got)
	}
}

// TestRoutingAppliesToLiveCore — принятое ядром правило доезжает до него
// перезапуском, а не ждёт переподключения.
func TestRoutingAppliesToLiveCore(t *testing.T) {
	c, _, runner := newTestCore(t)

	if _, err := c.AddManualProfile("тест", "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls&sni=example.com#node"); err != nil {
		t.Fatalf("профиль: %v", err)
	}
	runner.state = core.StateRunning

	id, err := c.AddRule(rules.Rule{
		Name: "тест", Enabled: true,
		Match: rules.MatchDomainSuffix, Values: []string{"example.com"},
		Action: rules.ActionDirect,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if id == "" {
		t.Fatal("AddRule не вернул ID")
	}
	if runner.restarts != 1 {
		t.Fatalf("ожидался один перезапуск ядра, было %d", runner.restarts)
	}
	if len(runner.lastConfig) == 0 {
		t.Fatal("в ядро уехал пустой конфиг")
	}
}

// TestRoutingSkipsStoppedCore — на остановленном ядре правки правил сохраняются
// молча, без попыток что-то перезапускать.
func TestRoutingSkipsStoppedCore(t *testing.T) {
	c, _, runner := newTestCore(t)

	if _, err := c.AddRule(rules.Rule{
		Name: "тест", Enabled: true,
		Match: rules.MatchDomainSuffix, Values: []string{"example.com"},
		Action: rules.ActionDirect,
	}); err != nil {
		t.Fatalf("AddRule на остановленном ядре: %v", err)
	}
	if runner.checks != 0 || runner.restarts != 0 {
		t.Fatalf("ядро тронуто на остановленном соединении: checks=%d restarts=%d",
			runner.checks, runner.restarts)
	}
}

// TestProfilesChangedEmitted — первый добавленный профиль становится активным
// внутри стора молча. Без события кнопка подключения оставалась серой до
// перезапуска приложения (чинили в 2.0.2).
func TestProfilesChangedEmitted(t *testing.T) {
	c, host, _ := newTestCore(t)

	if _, err := c.AddManualProfile("тест", "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=tcp&security=tls&sni=example.com#node"); err != nil {
		t.Fatalf("профиль: %v", err)
	}
	if !host.sawEvent("profiles:changed") {
		t.Fatal("событие profiles:changed не отправлено")
	}
	if c.GetActiveProfileID() == "" {
		t.Fatal("первый профиль не стал активным")
	}
}
