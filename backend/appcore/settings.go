package appcore

import (
	"strings"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/settings"
)

// GetSettings возвращает сохранённые настройки (для инициализации UI).
func (c *Core) GetSettings() settings.Settings {
	if c.settings == nil {
		return settings.Defaults()
	}
	return c.settings.Get()
}

// SetBlockQUIC включает/выключает резку QUIC в TUN (применяется при подключении).
func (c *Core) SetBlockQUIC(block bool) error {
	if c.settings == nil {
		return nil
	}
	return c.settings.Update(func(s *settings.Settings) { s.AllowQUIC = !block })
}

// RememberTUN запоминает выбранный режим перехвата на будущие запуски. Пишется
// только после того, как подключение действительно состоялось: сохранённый «TUN»
// при неудачном старте заставлял бы автозапуск и трей каждый раз лезть за правами
// администратора.
func (c *Core) RememberTUN(enableTUN bool) {
	if c.settings == nil {
		return
	}
	_ = c.settings.Update(func(s *settings.Settings) { s.EnableTUN = enableTUN })
}

// --- Язык интерфейса ---

// CurrentLang возвращает выбранный язык, а если пользователь его не выбирал —
// определённый по локали системы.
func (c *Core) CurrentLang() string {
	if c.settings != nil {
		if l := NormalizeLang(c.settings.Get().Lang); l != "" {
			return l
		}
	}
	return c.host.DefaultLang()
}

// NormalizeLang приводит язык к поддерживаемому значению; "" = не задан.
func NormalizeLang(lang string) string {
	switch strings.ToLower(lang) {
	case "ru":
		return "ru"
	case "en":
		return "en"
	}
	return ""
}

// SetLanguage запоминает язык интерфейса. Платформенную часть (меню трея на
// Windows) доделывает вызывающая сторона: оно живёт вне фронтенда и само на смену
// языка не отреагирует.
func (c *Core) SetLanguage(lang string) (string, error) {
	l := NormalizeLang(lang)
	if l == "" {
		return "", CodedErrf(ErrLangUnknown, "неизвестный язык: %q", lang)
	}
	if c.settings != nil {
		if err := c.settings.Update(func(s *settings.Settings) { s.Lang = l }); err != nil {
			return "", err
		}
	}
	return l, nil
}

// --- Уровень журнала ---

// LogLevels — уровни журнала ядра от самого подробного к самому тихому.
var LogLevels = []string{"trace", "debug", "info", "warn", "error"}

// GetLogLevel возвращает текущий уровень журнала ядра.
func (c *Core) GetLogLevel() string {
	if c.settings == nil {
		return config.DefaultLogLevel
	}
	return normalizeLogLevel(c.settings.Get().LogLevel)
}

// SetLogLevel меняет уровень журнала. Уровень зашит в конфиг, поэтому на живом ядре
// он применяется перезапуском — как и правки правил, без снятия прокси.
func (c *Core) SetLogLevel(level string) error {
	level = normalizeLogLevel(level)
	if c.settings == nil {
		return CodedErr(ErrNotReady, "настройки недоступны")
	}
	if err := c.settings.Update(func(s *settings.Settings) { s.LogLevel = level }); err != nil {
		return err
	}
	if err := c.ApplyRouting(); err != nil {
		return err
	}
	c.host.Emit("core:loglevel", level)
	return nil
}

// normalizeLogLevel приводит уровень к известному ядру значению.
func normalizeLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	for _, l := range LogLevels {
		if l == level {
			return level
		}
	}
	return config.DefaultLogLevel
}

// --- Автообновление подписок ---

// SetSubUpdateHours задаёт интервал автообновления подписок (0 — выключить).
func (c *Core) SetSubUpdateHours(hours int) error {
	if c.settings == nil {
		return nil
	}
	if hours < 0 {
		hours = 0
	}
	return c.settings.Update(func(s *settings.Settings) { s.SubUpdateHours = hours })
}

// StartSubScheduler раз в 30 минут проверяет подписки и обновляет те, что старше
// заданного интервала. Так смена интервала не требует перезапуска планировщика.
// Заодно тем же тиком докачиваются текстовые списки правил (.lst).
func (c *Core) StartSubScheduler() {
	go func() {
		// Первую проверку делаем вскоре после старта.
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
		c.autoRefreshSubs()
		c.refreshListSets()

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				c.autoRefreshSubs()
				c.refreshListSets()
			}
		}
	}()
}

func (c *Core) autoRefreshSubs() {
	if c.settings == nil || c.profiles == nil {
		return
	}
	hours := c.settings.Get().SubUpdateHours
	if hours <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	changed := false
	for _, p := range c.profiles.List() {
		if p.Kind == "subscription" && p.SubURL != "" && p.UpdatedAt.Before(cutoff) {
			if _, err := c.profiles.Refresh(c.ctx, p.ID); err == nil {
				changed = true
			}
		}
	}
	if changed {
		c.profilesChanged()
	}
}
