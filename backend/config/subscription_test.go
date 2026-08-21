package config

import (
	"context"
	"strings"
	"testing"
)

// Подписка — точка, из которой приложение узнаёт, куда отправлять весь трафик
// пользователя. Открытый http здесь означает, что список нод подменяет любой
// посредник (в первую очередь тот самый провайдер, от которого и защищаемся), а
// интерфейс при этом показывает штатное «подключено». Тест держит запрет на
// месте: соблазн «ну а если у панели нет сертификата» возникает регулярно.
func TestValidateSubscriptionURL(t *testing.T) {
	ok := []string{
		"https://example.com/sub",
		"HTTPS://example.com/sub?token=1",
		"  https://example.com/sub  ",
	}
	for _, u := range ok {
		if err := ValidateSubscriptionURL(u); err != nil {
			t.Errorf("ValidateSubscriptionURL(%q) = %v, ожидался пропуск", u, err)
		}
	}

	bad := []string{
		"http://example.com/sub", // главный случай: открытый канал
		"HTTP://example.com/sub", // схема сверяется без учёта регистра
		"file:///etc/passwd",     // чужая схема
		"ftp://example.com/sub",  //
		"example.com/sub",        // без схемы
		"",                       //
		"https:///sub",           // https, но без хоста
	}
	for _, u := range bad {
		if err := ValidateSubscriptionURL(u); err == nil {
			t.Errorf("ValidateSubscriptionURL(%q) = nil, ожидался отказ", u)
		}
	}
}

// Проверка обязана стоять внутри FetchSubscription, а не только у вызывающих:
// через неё идут добавление профиля, ручное обновление и тик планировщика
// подписок. Забыть её в одном из трёх мест ничего не мешает, а результат —
// молчаливая загрузка по открытому каналу.
func TestFetchSubscriptionRejectsPlainHTTP(t *testing.T) {
	// Адрес заведомо никуда не ведёт: если проверка отработала, до сети дело не
	// дойдёт вовсе, и тест не зависит от наличия интернета.
	_, err := FetchSubscription(context.Background(), "http://127.0.0.1:1/sub")
	if err == nil {
		t.Fatal("FetchSubscription по http вернул nil, ожидался отказ")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("ошибка не объясняет причину: %v", err)
	}
}
