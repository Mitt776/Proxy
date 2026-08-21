package appcore

// Проверка обновлений через GitHub Releases. Магазина у нас нет ни на одной
// платформе: Windows-сборка распространяется портативным архивом, Android — APK
// для ручной установки, и узнать о новой версии пользователю больше неоткуда.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"Proxy/backend/settings"
)

// releasesURL — последний выпуск в репозитории проекта.
const releasesURL = "https://api.github.com/repos/Mitt776/Proxy/releases/latest"

// updateCheckEvery — как часто спрашиваем GitHub. Чаще незачем: релизы выходят
// хорошо если раз в месяц, а у анонимных запросов к API лимит 60 в час на адрес.
const updateCheckEvery = 24 * time.Hour

// UpdateInfo — итог проверки. Available=false означает «свежее нет»; это не
// ошибка, поэтому отдельного признака «проверка не удалась» здесь нет — неудача
// приходит обычной ошибкой.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Notes     string `json:"notes"`
}

// SetUpdateCheck включает или выключает ежесуточную проверку.
func (c *Core) SetUpdateCheck(on bool) error {
	if c.settings == nil {
		return nil
	}
	return c.settings.Update(func(s *settings.Settings) { s.NoUpdateCheck = !on })
}

// UpdateCheckEnabled — включена ли проверка (по умолчанию да, см. NoUpdateCheck).
func (c *Core) UpdateCheckEnabled() bool {
	if c.settings == nil {
		return true
	}
	return !c.settings.Get().NoUpdateCheck
}

// CheckUpdate спрашивает GitHub прямо сейчас, не глядя на расписание и на
// выключатель: это ручная проверка по кнопке из настроек.
func (c *Core) CheckUpdate() (UpdateInfo, error) {
	return c.fetchUpdate(c.ctx)
}

// StartUpdateScheduler раз в сутки проверяет обновления и, если нашлось,
// сообщает интерфейсу событием update:available.
//
// Отдельная горутина, а не тик планировщика подписок: у того период 30 минут и
// своя задача, а смешивать «сходить в интернет за нодами пользователя» и
// «сходить на GitHub» не стоит — выключаются они независимо.
func (c *Core) StartUpdateScheduler() {
	go func() {
		// Первую проверку делаем не сразу: на старте и без неё есть чем занять
		// сеть — подписки, наборы правил, само подключение.
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		c.updateTick()

		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				c.updateTick()
			}
		}
	}()
}

// updateTick проверяет обновления, если проверка включена и с прошлого раза
// прошли сутки. Тикаем чаще срока проверки, чтобы включение выключателя не ждало
// сутки до первого эффекта.
func (c *Core) updateTick() {
	if c.settings == nil || !c.UpdateCheckEnabled() {
		return
	}
	last := c.settings.Get().LastUpdateCheck
	if !last.IsZero() && time.Since(last) < updateCheckEvery {
		return
	}

	info, err := c.fetchUpdate(c.ctx)
	// Время проверки пишем и при ошибке: без сети запрос падает мгновенно, и без
	// отметки мы долбили бы GitHub каждый час на каждом тике.
	_ = c.settings.Update(func(s *settings.Settings) { s.LastUpdateCheck = time.Now() })
	if err != nil {
		c.LogLine("проверка обновлений: " + err.Error())
		return
	}
	if info.Available {
		c.host.Emit("update:available", info)
	}
}

// ghRelease — то немногое, что нам нужно из ответа GitHub.
type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (c *Core) fetchUpdate(ctx context.Context) (UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", releasesURL, nil)
	if err != nil {
		return UpdateInfo{}, CodedErrf(ErrUpdateCheck, "%w", err)
	}
	// GitHub отвечает 403 на запросы без User-Agent — это не рекомендация, а
	// требование их API.
	req.Header.Set("User-Agent", "MitM/"+AppVersion)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}, CodedErrf(ErrUpdateCheck, "%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, CodedErrf(ErrUpdateCheck, "GitHub ответил %s", resp.Status)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return UpdateInfo{}, CodedErrf(ErrUpdateCheck, "%w", err)
	}
	if rel.Draft || rel.Prerelease {
		return UpdateInfo{}, nil
	}

	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	return UpdateInfo{
		Available: newerVersion(latest, AppVersion),
		Version:   latest,
		URL:       releasePageURL(rel.HTMLURL),
		Notes:     rel.Body,
	}, nil
}

// releasePageURL проверяет адрес выпуска, приехавший в ответе GitHub.
//
// По этой ссылке пользователь уходит одним нажатием, и приложение отдаёт её
// системе — то есть содержимое чужого ответа определяет, что откроется. Ответ
// приходит по TLS от api.github.com, так что подменить его непросто, но цена
// проверки — три строки, а цена ошибки — открытая по нашей команде произвольная
// ссылка. Всё, что не похоже на страницу релиза на github.com, заменяем на
// список выпусков: он всегда ведёт куда надо.
func releasePageURL(raw string) string {
	const fallback = "https://github.com/Mitt776/Proxy/releases"
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return fallback
	}
	if host := strings.ToLower(u.Hostname()); host != "github.com" && !strings.HasSuffix(host, ".github.com") {
		return fallback
	}
	return u.String()
}

// newerVersion сравнивает версии вида «2.1.0» покомпонентно.
//
// Строковое сравнение здесь не годится: «2.10.0» лексикографически меньше
// «2.9.0», и после десятого минорного выпуска обновления бы просто перестали
// замечаться. Непонятную часть считаем нулём — тег без номера не должен
// объявлять обновление.
func newerVersion(latest, current string) bool {
	a, b := versionParts(latest), versionParts(current)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	// Хвост вроде «2.1.0-rc1» отбрасываем целиком: предрелизы мы и так отсеяли
	// по флагу prerelease, а сравнивать их номера незачем.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, part := range strings.SplitN(v, ".", 3) {
		if i >= len(out) {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return out
		}
		out[i] = n
	}
	return out
}
