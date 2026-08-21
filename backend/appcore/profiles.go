package appcore

import (
	"encoding/json"

	"Proxy/backend/config"
	"Proxy/backend/profile"
)

// ListProfiles возвращает все профили.
func (c *Core) ListProfiles() []*profile.Profile {
	if c.profiles == nil {
		return nil
	}
	return c.profiles.List()
}

// GetActiveProfileID возвращает id активного профиля.
func (c *Core) GetActiveProfileID() string {
	if c.profiles == nil {
		return ""
	}
	return c.profiles.ActiveID()
}

// profilesChanged — сообщить платформе и UI, что список профилей или активный
// профиль изменились.
//
// Событие нужно не только вкладке «Профили» (она перечитывает список сама): на нём
// висит имя активного профиля в оболочке, а от него — доступность кнопки
// подключения. Первый добавленный профиль становится активным внутри стора молча,
// поэтому без события кнопка оставалась серой до перезапуска приложения.
func (c *Core) profilesChanged() {
	c.host.ProfilesChanged()
	c.host.Emit("profiles:changed", nil)
}

// AddManualProfile создаёт ручной профиль из ссылок/JSON.
func (c *Core) AddManualProfile(name, raw string) (*profile.Profile, error) {
	if c.profiles == nil {
		return nil, CodedErr(ErrNotReady, "хранилище не готово")
	}
	p, err := c.profiles.AddManual(name, raw)
	if err == nil {
		c.profilesChanged()
	}
	return p, err
}

// AddSubscriptionProfile создаёт профиль-подписку по URL.
func (c *Core) AddSubscriptionProfile(name, url string) (*profile.Profile, error) {
	if c.profiles == nil {
		return nil, CodedErr(ErrNotReady, "хранилище не готово")
	}
	// Отказ по http ловим здесь, а не только в config.FetchSubscription: там он
	// тоже стоит (единственная точка для планировщика), но ошибка без кода
	// приехала бы в интерфейс непереведённой.
	if err := config.ValidateSubscriptionURL(url); err != nil {
		return nil, CodedErrf(ErrInsecureURL, "%w", err)
	}
	p, err := c.profiles.AddSubscription(c.ctx, name, url)
	if err == nil {
		c.profilesChanged()
	}
	return p, err
}

// RefreshProfile перезагружает подписку.
func (c *Core) RefreshProfile(id string) (*profile.Profile, error) {
	if c.profiles == nil {
		return nil, CodedErr(ErrNotReady, "хранилище не готово")
	}
	// Профиль мог быть заведён до запрета http — тогда обновление отказывает с
	// объяснением, а не молча тянет подписку по открытому каналу.
	if p := c.profiles.Get(id); p != nil && p.SubURL != "" {
		if err := config.ValidateSubscriptionURL(p.SubURL); err != nil {
			return nil, CodedErrf(ErrInsecureURL, "%w", err)
		}
	}
	p, err := c.profiles.Refresh(c.ctx, id)
	if err == nil {
		c.profilesChanged()
	}
	return p, err
}

// DeleteProfile удаляет профиль.
func (c *Core) DeleteProfile(id string) error {
	if c.profiles == nil {
		return CodedErr(ErrNotReady, "хранилище не готово")
	}
	err := c.profiles.Delete(id)
	if err == nil {
		c.profilesChanged()
	}
	return err
}

// SetActiveProfile помечает профиль активным.
//
// На живом соединении смена профиля обязана менять и трафик: раньше метод только
// записывал ID, ядро продолжало работать на прежних нодах, и «Сменить профиль»
// молча ничего не делало до переподключения. Пересборка идёт тем же путём, что и
// правки правил (ApplyRouting → Runner.Restart), поэтому системный прокси с
// активного соединения не слетает. Не принятый ядром профиль откатывается.
func (c *Core) SetActiveProfile(id string) error {
	if c.profiles == nil {
		return CodedErr(ErrNotReady, "хранилище не готово")
	}
	prev := c.profiles.ActiveID()
	if err := c.profiles.SetActive(id); err != nil {
		return err
	}
	c.host.ProfilesChanged()

	if err := c.ApplyRouting(); err != nil {
		_ = c.profiles.SetActive(prev)
		c.host.ProfilesChanged()
		return err
	}
	c.profilesChanged()
	return nil
}

// NodeInfo — краткое описание ноды для UI.
type NodeInfo struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

// ListProfileNodes возвращает ноды профиля (для выбора в UI).
func (c *Core) ListProfileNodes(id string) ([]NodeInfo, error) {
	if c.profiles == nil {
		return nil, CodedErr(ErrNotReady, "хранилище не готово")
	}
	nodes, err := c.profiles.ResolveNodes(id)
	if err != nil {
		return nil, err
	}
	return nodeInfos(nodes), nil
}

// ProfileConfigJSON возвращает готовый config.json sing-box для профиля
// (в mixed-режиме, с текущими настройками маршрутизации) — для копирования/шаринга.
func (c *Core) ProfileConfigJSON(id string) (string, error) {
	if c.profiles == nil {
		return "", CodedErr(ErrNotReady, "хранилище не готово")
	}
	nodes, err := c.profiles.ResolveNodes(id)
	if err != nil {
		return "", err
	}
	cfg, err := config.Generate(c.ConfigOptions(nodes, false))
	if err != nil {
		return "", err
	}
	return string(cfg), nil
}

// ProfileRaw возвращает исходный ввод профиля (ссылки/JSON или тело подписки).
func (c *Core) ProfileRaw(id string) (string, error) {
	if c.profiles == nil {
		return "", CodedErr(ErrNotReady, "хранилище не готово")
	}
	p := c.profiles.Get(id)
	if p == nil {
		return "", CodedErr(ErrProfileNotFound, "профиль не найден")
	}
	return p.Raw, nil
}

// ActiveNodes возвращает ноды активного профиля — то, на чём поднимается ядро.
func (c *Core) ActiveNodes() ([]config.Node, error) {
	if c.profiles == nil {
		return nil, CodedErr(ErrNotReady, "приложение не инициализировано")
	}
	activeID := c.profiles.ActiveID()
	if activeID == "" {
		return nil, CodedErr(ErrNoProfile, "не выбран активный профиль")
	}
	return c.profiles.ResolveNodes(activeID)
}

func nodeInfos(nodes []config.Node) []NodeInfo {
	out := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		info := NodeInfo{Tag: n.Tag}
		var meta struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(n.Outbound, &meta)
		info.Type = meta.Type
		out = append(out, info)
	}
	return out
}
