package profile

import "testing"

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
