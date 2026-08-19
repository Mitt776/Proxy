package system

import (
	"strings"
	"testing"
)

// TestListProcesses проверяет пикер процессов на живой системе: список не
// пустой, одноимённые процессы схлопнуты, и хотя бы у одного нашлась иконка
// (иначе в UI будет колонка пустых квадратов).
func TestListProcesses(t *testing.T) {
	list, err := ListProcesses()
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("список процессов пуст")
	}

	seen := map[string]bool{}
	icons, paths := 0, 0
	for _, p := range list {
		key := strings.ToLower(p.Name)
		if seen[key] {
			t.Errorf("процесс %q встретился дважды — одноимённые должны схлопываться", p.Name)
		}
		seen[key] = true
		if p.Path != "" {
			paths++
		}
		if strings.HasPrefix(p.Icon, "data:image/png;base64,") {
			icons++
		} else if p.Icon != "" {
			t.Errorf("процесс %q: иконка не data-URL: %.40s", p.Name, p.Icon)
		}
	}
	if paths == 0 {
		t.Error("ни у одного процесса не определился путь")
	}
	if icons == 0 {
		t.Error("ни у одного процесса не извлеклась иконка")
	}
	t.Logf("процессов: %d, с путём: %d, с иконкой: %d", len(list), paths, icons)
}
