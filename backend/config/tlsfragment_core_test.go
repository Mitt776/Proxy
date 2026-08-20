//go:build coretest

package config

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"Proxy/backend/rules"
)

// chunk — один кусок, которым до сервера доехал ClientHello.
type chunk struct {
	size int
	at   time.Duration
}

// recordServer поднимает фейковый TLS-сервер на 127.0.0.1: принимает одно
// соединение и записывает, сколько байт пришло и когда. Отвечать не нужно —
// нас интересует только форма первой записи клиента.
func recordServer(t *testing.T) (int, <-chan []chunk) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	out := make(chan []chunk, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			out <- nil
			return
		}
		defer conn.Close()
		start := time.Now()
		var got []chunk
		buf := make([]byte, 16*1024)
		for {
			// Полторы секунды тишины = ClientHello доехал целиком. Ядро при
			// фрагментации выдерживает паузу между кусками, поэтому дедлайн
			// продлевается после каждого чтения.
			_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
			n, err := conn.Read(buf)
			if n > 0 {
				got = append(got, chunk{size: n, at: time.Since(start)})
			}
			if err != nil {
				break
			}
		}
		out <- got
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

// socks5Dial проходит рукопожатие SOCKS5 на локальном mixed-inbound-е ядра и
// просит соединение с 127.0.0.1:dstPort.
func socks5Dial(t *testing.T, proxyPort, dstPort int) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 3*time.Second)
	if err != nil {
		t.Fatalf("подключение к inbound-у ядра: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil || resp[1] != 0x00 {
		t.Fatalf("SOCKS5 greeting: %v %v", err, resp)
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(req[8:], uint16(dstPort))
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatalf("SOCKS5 reply: %v", err)
	}
	if rep[1] != 0x00 {
		t.Fatalf("SOCKS5 отказал, код %d", rep[1])
	}
	_ = c.SetDeadline(time.Time{})
	return c
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// runCore запускает ядро с готовым конфигом и ждёт, пока поднимется inbound.
func runCore(t *testing.T, bin string, opts Options) {
	t.Helper()
	cfg, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", cfgPath, "-D", dir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("запуск ядра: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	addr := fmt.Sprintf("127.0.0.1:%d", opts.MixedPort)
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("ядро не подняло inbound на %s", addr)
}

// fragmentChunks гонит настоящий ClientHello через ядро и возвращает куски,
// которыми он доехал до фейкового сервера.
func fragmentChunks(t *testing.T, bin string, frag bool) []chunk {
	t.Helper()
	dstPort, result := recordServer(t)
	mixed := freePort(t)

	runCore(t, bin, Options{
		MixedPort:    mixed,
		ClashAPIPort: freePort(t),
		ClashSecret:  "x",
		LogLevel:     "warn",
		RuleSetDir:   os.Getenv("PROXY_ASSETS"),
		CacheDBPath:  "cache.db",
		Routing: rules.Config{
			Version: rules.Version,
			Final:   rules.ActionDirect,
			Rules: []rules.Rule{{
				Enabled:     true,
				Match:       rules.MatchPort,
				Values:      []string{fmt.Sprint(dstPort)},
				Action:      rules.ActionDirect,
				TLSFragment: frag,
			}},
		},
	})

	conn := socks5Dial(t, mixed, dstPort)
	defer conn.Close()

	// Настоящий ClientHello от crypto/tls: ядру нужна валидная TLS-запись с SNI,
	// по нему оно и выбирает точку разреза. Рукопожатие провалится (сервер
	// молчит), нам нужен только исходящий пакет.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tls.Client(conn, &tls.Config{
			ServerName:         "www.example.com",
			InsecureSkipVerify: true,
		}).Handshake()
	}()

	select {
	case got := <-result:
		return got
	case <-time.After(20 * time.Second):
		t.Fatal("сервер не дождался ClientHello")
		return nil
	}
}

func sizes(cs []chunk) []int {
	out := make([]int, len(cs))
	for i, c := range cs {
		out[i] = c.size
	}
	return out
}

func total(cs []chunk) int {
	n := 0
	for _, c := range cs {
		n += c.size
	}
	return n
}

// TestTLSFragmentActuallyFragments — эмпирическая проверка того, что галка
// «Резать TLS-приветствие» реально что-то делает, а не просто принимается ядром
// при `check`. Один и тот же ClientHello гоняется через ядро дважды: без
// tls_fragment он обязан доехать одной записью, с ним — несколькими.
func TestTLSFragmentActuallyFragments(t *testing.T) {
	for name, bin := range cores(t) {
		t.Run(name, func(t *testing.T) {
			plain := fragmentChunks(t, bin, false)
			if len(plain) == 0 {
				t.Fatal("без фрагментации до сервера не доехало ничего")
			}
			t.Logf("без tls_fragment: %d кусок(ов) %v", len(plain), sizes(plain))
			if len(plain) != 1 {
				t.Fatalf("без tls_fragment ClientHello должен приходить одной записью, пришло %d", len(plain))
			}

			frag := fragmentChunks(t, bin, true)
			if len(frag) == 0 {
				t.Fatal("с фрагментацией до сервера не доехало ничего")
			}
			t.Logf("с tls_fragment:   %d кусок(ов) %v, последний в %v",
				len(frag), sizes(frag), frag[len(frag)-1].at)
			if len(frag) < 2 {
				t.Fatalf("tls_fragment не сработал: ClientHello пришёл %d записью", len(frag))
			}
			if total(frag) != total(plain) {
				t.Fatalf("фрагментация исказила ClientHello: %d байт против %d", total(frag), total(plain))
			}
			t.Logf("✅ %s режет ClientHello на %d частей, суммарно те же %d байт",
				name, len(frag), total(frag))
		})
	}
}
