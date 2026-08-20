package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManualProfileLifecycle(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	raw := "trojan://p@a.example.com:443#A\nvless://11111111-1111-1111-1111-111111111111@b.example.com:443?security=reality&pbk=x&sni=c.com#B"
	p, err := s.AddManual("Мой профиль", raw)
	if err != nil {
		t.Fatalf("AddManual: %v", err)
	}
	if p.NodeCount != 2 {
		t.Errorf("NodeCount = %d, want 2", p.NodeCount)
	}
	if s.ActiveID() != p.ID {
		t.Errorf("первый профиль должен стать активным")
	}

	// Резолв нод.
	nodes, err := s.ResolveNodes(p.ID)
	if err != nil {
		t.Fatalf("ResolveNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("резолв дал %d нод, want 2", len(nodes))
	}

	// Персистентность: перечитываем с диска.
	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.List(); len(got) != 1 || got[0].Name != "Мой профиль" {
		t.Fatalf("после перезагрузки профиль потерян: %+v", got)
	}
	if s2.ActiveID() != p.ID {
		t.Errorf("активный профиль не сохранился")
	}

	// Удаление.
	if err := s2.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(s2.List()) != 0 {
		t.Errorf("профиль не удалился")
	}
	if s2.ActiveID() != "" {
		t.Errorf("активный id должен сброситься после удаления")
	}
}

func TestAddManualRejectsGarbage(t *testing.T) {
	s, _ := Load(t.TempDir())
	if _, err := s.AddManual("bad", "это не ссылка и не json"); err == nil {
		t.Errorf("ожидалась ошибка на мусорном вводе")
	}
}

// TestLoadCorruptedFile: битый profiles.json не должен ронять приложение и не
// должен молча пропадать. Load обязан вернуть рабочее (пустое) хранилище,
// ошибку — наверх, а прежний файл отложить рядом.
func TestLoadCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	raw := `{"profiles": [сломано`
	if err := os.WriteFile(filepath.Join(dir, "profiles.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(dir)
	if err == nil {
		t.Fatal("ожидалась ошибка о повреждённом файле")
	}
	if s == nil {
		t.Fatal("Load вернул nil-хранилище: вызовы App упадут на разыменовании")
	}
	if len(s.List()) != 0 {
		t.Fatalf("ожидался пустой список профилей, получено %d", len(s.List()))
	}
	bad, rerr := os.ReadFile(filepath.Join(dir, "profiles.json.bad"))
	if rerr != nil {
		t.Fatalf("битый файл не сохранён как profiles.json.bad: %v", rerr)
	}
	if string(bad) != raw {
		t.Fatalf("в profiles.json.bad не исходное содержимое: %q", bad)
	}

	// Хранилище остаётся рабочим: новый профиль сохраняется поверх.
	if _, err := s.AddManual("Новый", "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com#n"); err != nil {
		t.Fatalf("хранилище после сбоя нерабочее: %v", err)
	}
}

// TestListReturnsCopies: List отдаёт копии, а не указатели на внутренние
// объекты — иначе трей и планировщик подписок читают то, что Refresh правит.
func TestListReturnsCopies(t *testing.T) {
	s, _ := Load(t.TempDir())
	if _, err := s.AddManual("Профиль", "vless://11111111-1111-1111-1111-111111111111@x.com:443?security=tls&sni=x.com#n"); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	list[0].Name = "подменено"

	if got := s.List()[0].Name; got == "подменено" {
		t.Fatal("List отдал указатель на внутренний профиль — его правка утекла в хранилище")
	}
}

// TestDeleteActivePromotesNext: удаление активного профиля не должно оставлять
// хранилище с профилями, но без активного — в таком состоянии UI показывает
// список нод и при этом не даёт подключиться.
func TestDeleteActivePromotesNext(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.AddManual("Первый", "trojan://p@a.example.com:443#A")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddManual("Второй", "trojan://p@b.example.com:443#B")
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveID() != first.ID {
		t.Fatalf("активным должен быть первый профиль")
	}

	if err := s.Delete(first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.ActiveID() != second.ID {
		t.Errorf("активный id = %q, ожидался оставшийся профиль %q", s.ActiveID(), second.ID)
	}
}
