package appcore

// Удалённые наборы правил и текстовые списки доменов (.lst).

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"Proxy/backend/config"
	"Proxy/backend/rules"
)

// ListRuleSets возвращает описанные удалённые наборы правил.
func (c *Core) ListRuleSets() []rules.RuleSet {
	if c.rules == nil {
		return nil
	}
	return c.rules.Get().RuleSets
}

// AddRuleSet добавляет удалённый набор правил и возвращает его ID.
// Как и правки правил, идёт через withRouting: не принятый ядром набор
// откатывается, а не остаётся висеть в routing.json.
func (c *Core) AddRuleSet(rs rules.RuleSet) (string, error) {
	// Список качаем до записи в routing.json: битый адрес должен вернуть ошибку
	// сразу, а не превратиться в набор, который ядро молча проигнорирует.
	if err := c.syncListSet(rs); err != nil {
		return "", err
	}
	var id string
	err := c.withRouting(func() error {
		var e error
		id, e = c.rules.AddRuleSet(rs)
		return e
	})
	if err != nil {
		config.RemoveListSet(c.ListSetDir(), rs.Tag)
		return "", err
	}
	return id, nil
}

// UpdateRuleSet заменяет описание набора.
func (c *Core) UpdateRuleSet(rs rules.RuleSet) error {
	prev := c.findRuleSetByID(rs.ID)
	if err := c.syncListSet(rs); err != nil {
		return err
	}
	if err := c.withRouting(func() error { return c.rules.UpdateRuleSet(rs) }); err != nil {
		return err
	}
	// Тег или формат поменялись — старый файл больше никому не нужен.
	if prev != nil && prev.IsList() && (prev.Tag != rs.Tag || !rs.IsList()) {
		config.RemoveListSet(c.ListSetDir(), prev.Tag)
	}
	return nil
}

// DeleteRuleSet удаляет описание набора (если на него никто не ссылается).
func (c *Core) DeleteRuleSet(id string) error {
	prev := c.findRuleSetByID(id)
	if err := c.withRouting(func() error { return c.rules.DeleteRuleSet(id) }); err != nil {
		return err
	}
	if prev != nil && prev.IsList() {
		config.RemoveListSet(c.ListSetDir(), prev.Tag)
	}
	return nil
}

// RefreshRuleSet перекачивает список по кнопке в UI и возвращает число доменов.
func (c *Core) RefreshRuleSet(id string) (int, error) {
	rs := c.findRuleSetByID(id)
	if rs == nil {
		return 0, CodedErr(ErrSetNotFound, "набор правил не найден")
	}
	if !rs.IsList() {
		return 0, CodedErr(ErrSetNotList, "обновлять вручную можно только текстовые списки")
	}
	n, err := c.fetchListSet(*rs)
	if err != nil {
		return 0, err
	}
	// Набор мог не попасть в прошлый конфиг из-за отсутствия файла — после удачной
	// загрузки перегенерируем, чтобы правило заработало без переподключения.
	if err := c.ApplyRouting(); err != nil {
		return n, err
	}
	return n, nil
}

func (c *Core) findRuleSetByID(id string) *rules.RuleSet {
	if c.rules == nil || id == "" {
		return nil
	}
	sets := c.rules.Get().RuleSets
	for i := range sets {
		if sets[i].ID == id {
			return &sets[i]
		}
	}
	return nil
}

// syncListSet качает список, если набор действительно списочный. Для обычных
// наборов — тихо ничего не делает, чтобы вызывающим не пришлось разбирать типы.
func (c *Core) syncListSet(rs rules.RuleSet) error {
	if !rs.IsList() {
		return nil
	}
	_, err := c.fetchListSet(rs)
	return err
}

func (c *Core) fetchListSet(rs rules.RuleSet) (int, error) {
	dir := c.ListSetDir()
	if dir == "" {
		return 0, CodedErr(ErrNotReady, "каталог данных недоступен")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()

	n, err := config.SyncListSet(ctx, rs, dir, c.listSetClient(rs))
	if err != nil {
		return 0, CodedErrf(ErrSetFetch, "не удалось загрузить список %q: %w", rs.Tag, err)
	}
	return n, nil
}

// listSetClient выбирает, чем качать список. «Через прокси» имеет смысл только на
// живом ядре: до подключения локальный порт никто не слушает, и запрос упал бы
// вместо того, чтобы просто пойти напрямую.
func (c *Core) listSetClient(rs rules.RuleSet) *http.Client {
	client := &http.Client{Timeout: 60 * time.Second}
	if rs.Detour != rules.ActionProxy || !c.running() {
		return client
	}
	u, err := url.Parse("http://" + c.ProxyAddr())
	if err != nil {
		return client
	}
	client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	return client
}

// refreshListSets перекачивает списки, которых нет на диске или которые старше
// своего интервала. Зовётся из планировщика подписок — отдельный таймер ради этого
// заводить не за чем, периодичность там та же по смыслу.
//
// Конфиг намеренно не пересобирается: ядро читает локальный набор при старте, и
// применить обновление можно только перезапуском. Дёргать его в фоне, посреди
// чужого видеозвонка, ради суточного обновления списка — плохой обмен. Новое
// содержимое подхватится при следующем подключении или любой правке правил, а кому
// нужно сейчас — есть кнопка (RefreshRuleSet, там перезапуск осознанный).
func (c *Core) refreshListSets() {
	if c.rules == nil {
		return
	}
	dir := c.ListSetDir()
	if dir == "" {
		return
	}
	for _, rs := range c.rules.Get().RuleSets {
		if !rs.IsList() {
			continue
		}
		if fresh(config.ListSetPath(dir, rs.Tag), rs.UpdateHours) {
			continue
		}
		if n, err := c.fetchListSet(rs); err != nil {
			c.LogLine(fmt.Sprintf("набор правил %s: %v", rs.Tag, err))
		} else {
			c.LogLine(fmt.Sprintf("набор правил %s обновлён: %d записей", rs.Tag, n))
		}
	}
}

// LogLine кладёт строку приложения в тот же поток, что и журнал ядра: фоновая
// докачка списков происходит без участия пользователя, и единственное место, где о
// ней можно узнать, — вкладка «Журнал».
func (c *Core) LogLine(line string) {
	c.host.Emit("core:log", time.Now().Format("2006-01-02 15:04:05")+" "+line)
}

// fresh сообщает, что файл на диске моложе интервала обновления.
func fresh(path string, updateHours int) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	h := updateHours
	if h <= 0 {
		h = 24
	}
	return time.Since(st.ModTime()) < time.Duration(h)*time.Hour
}
