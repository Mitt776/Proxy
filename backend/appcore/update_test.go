package appcore

import "testing"

// Сравнение версий — единственное место проверки обновлений, где ошибка тихая:
// приложение просто перестаёт замечать новые выпуски, и узнать об этом неоткуда.
func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"2.1.0", "2.0.2", true},
		{"2.0.2", "2.0.2", false},
		{"2.0.1", "2.0.2", false},
		{"3.0.0", "2.9.9", true},
		{"2.2.0", "2.10.0", false},
		// Ради этого случая покомпонентное сравнение и написано: строкой
		// "2.10.0" < "2.9.0", и после десятого минорного выпуска обновления
		// перестали бы находиться.
		{"2.10.0", "2.9.0", true},
		{"2.1", "2.0.2", true},
		{"2.1.0-rc1", "2.1.0", false},
		// Тег без номера не должен объявлять обновление.
		{"latest", "2.1.0", false},
		{"", "2.1.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, ожидалось %v", c.latest, c.current, got, c.want)
		}
	}
}

// SetExcludedApps чистит список перед записью: из интерфейса он приходит как есть,
// а в VpnService.Builder дубликат пакета — это исключение при establish().
func TestSetExcludedAppsNormalizes(t *testing.T) {
	c, _, _ := newTestCore(t)

	if err := c.SetExcludedApps([]string{"com.b", " com.a ", "com.b", "", "   "}); err != nil {
		t.Fatalf("SetExcludedApps: %v", err)
	}
	got := c.GetExcludedApps()
	want := []string{"com.a", "com.b"}
	if len(got) != len(want) {
		t.Fatalf("получили %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("получили %v, ожидалось %v", got, want)
		}
	}
}

// Проверка обновлений включена по умолчанию: у settings.json старых версий поля
// нет вовсе, и нулевое значение обязано означать «включено».
func TestUpdateCheckEnabledByDefault(t *testing.T) {
	c, _, _ := newTestCore(t)

	if !c.UpdateCheckEnabled() {
		t.Fatal("по умолчанию проверка обновлений должна быть включена")
	}
	if err := c.SetUpdateCheck(false); err != nil {
		t.Fatalf("SetUpdateCheck: %v", err)
	}
	if c.UpdateCheckEnabled() {
		t.Fatal("после выключения проверка обновлений должна быть выключена")
	}
}
