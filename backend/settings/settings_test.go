package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Get().MinimizeToTray {
		t.Error("по умолчанию окно должно сворачиваться в трей")
	}

	if err := s.Update(func(c *Settings) { c.CorePath = `D:\assets\sing-box-xhttp.exe`; c.LogLevel = "debug" }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatalf("повторный Load: %v", err)
	}
	if got := again.Get(); got.CorePath == "" || got.LogLevel != "debug" {
		t.Fatalf("настройки не долетели до диска: %+v", got)
	}
}

// TestLoadCorruptedFile: битый settings.json не роняет приложение (иначе
// пользователь остаётся без GUI из-за одного испорченного файла), но и не
// исчезает молча — Load сообщает об ошибке и откладывает файл рядом.
func TestLoadCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	raw := `{"corePath": "D:\broken`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(dir)
	if err == nil {
		t.Fatal("ожидалась ошибка о повреждённом файле")
	}
	if s == nil {
		t.Fatal("Load вернул nil-хранилище: App упадёт на первом обращении к настройкам")
	}
	if got := s.Get(); !got.MinimizeToTray || got.CorePath != "" {
		t.Fatalf("после битого файла ожидались дефолты, получено %+v", got)
	}
	if _, rerr := os.Stat(filepath.Join(dir, "settings.json.bad")); rerr != nil {
		t.Fatalf("битый файл не сохранён как settings.json.bad: %v", rerr)
	}

	// Хранилище рабочее: запись поверх проходит и читается обратно.
	if err := s.Update(func(c *Settings) { c.SubUpdateHours = 12 }); err != nil {
		t.Fatalf("Update после сбоя: %v", err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatalf("повторный Load: %v", err)
	}
	if again.Get().SubUpdateHours != 12 {
		t.Fatal("настройки не сохранились после восстановления")
	}
}
