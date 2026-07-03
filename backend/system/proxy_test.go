//go:build coretest

package system

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// TestSystemProxySetClear проверяет реальную запись/снятие системного прокси.
// Исходные значения реестра сохраняются и восстанавливаются в defer, поэтому
// после теста настройки пользователя гарантированно возвращаются как были.
func TestSystemProxySetClear(t *testing.T) {
	k, err := registry.OpenKey(registry.CURRENT_USER, proxyRegPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatalf("открытие реестра: %v", err)
	}
	defer k.Close()

	// Сырой бэкап для безусловного восстановления.
	origEnable, _, _ := k.GetIntegerValue("ProxyEnable")
	origServer, _, _ := k.GetStringValue("ProxyServer")
	origOverride, _, _ := k.GetStringValue("ProxyOverride")
	defer func() {
		_ = k.SetDWordValue("ProxyEnable", uint32(origEnable))
		_ = k.SetStringValue("ProxyServer", origServer)
		_ = k.SetStringValue("ProxyOverride", origOverride)
		notifyWinInet()
	}()

	sp := NewSystemProxy()

	if err := sp.Set("127.0.0.1:2080"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !sp.Active() {
		t.Errorf("Active() = false после Set")
	}
	if en, _, _ := k.GetIntegerValue("ProxyEnable"); en != 1 {
		t.Errorf("ProxyEnable = %d, want 1", en)
	}
	if srv, _, _ := k.GetStringValue("ProxyServer"); srv != "127.0.0.1:2080" {
		t.Errorf("ProxyServer = %q, want 127.0.0.1:2080", srv)
	}
	t.Log("✅ системный прокси включён и записан в реестр")

	if err := sp.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if sp.Active() {
		t.Errorf("Active() = true после Clear")
	}
	if en, _, _ := k.GetIntegerValue("ProxyEnable"); en != uint64(origEnable) {
		t.Errorf("после Clear ProxyEnable = %d, want исходный %d", en, origEnable)
	}
	t.Log("✅ системный прокси снят, исходное состояние восстановлено")
}
