package system

import "testing"

// TestSameProxyAddr: по значению ProxyServer из реестра надо уметь понять, наш
// это прокси или чужой. От этого зависит, снимем ли мы на старте настройку,
// оставшуюся от аварийно завершённого запуска, — и не тронем ли чужую.
func TestSameProxyAddr(t *testing.T) {
	const addr = "127.0.0.1:2080"
	cases := []struct {
		name   string
		server string
		want   bool
	}{
		{"наш адрес", "127.0.0.1:2080", true},
		{"наш адрес с пробелами", "  127.0.0.1:2080 ", true},
		{"по схемам", "http=127.0.0.1:2080;https=127.0.0.1:2080", true},
		{"наш только в одной схеме", "http=10.0.0.1:8080;https=127.0.0.1:2080", true},
		{"чужой прокси", "10.0.0.1:8080", false},
		{"другой порт", "127.0.0.1:1080", false},
		{"пусто", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameProxyAddr(c.server, addr); got != c.want {
				t.Fatalf("sameProxyAddr(%q, %q) = %v, want %v", c.server, addr, got, c.want)
			}
		})
	}
	if sameProxyAddr("127.0.0.1:2080", "") {
		t.Fatal("с пустым собственным адресом ничего не должно считаться нашим")
	}
}

// TestClearOnUntouchedRegistry: пока мы прокси не ставили, Clear не должен
// трогать реестр вообще — чужие настройки не наше дело.
func TestClearOnUntouchedRegistry(t *testing.T) {
	s := NewSystemProxy()
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear на нетронутом прокси вернул ошибку: %v", err)
	}
	if s.Active() {
		t.Fatal("прокси не ставили — Active должен быть false")
	}
}
