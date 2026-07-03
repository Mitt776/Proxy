// Package profile хранит профили конфигураций (ручные и подписки) на диске.
package profile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Proxy/backend/config"
)

// Profile — один профиль: набор нод из ручного ввода или из подписки.
type Profile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "manual" | "subscription"
	SubURL    string    `json:"subUrl,omitempty"`
	Raw       string    `json:"raw"` // сырой ввод (ссылки/JSON) или тело подписки
	NodeCount int       `json:"nodeCount"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type storeData struct {
	Profiles []*Profile `json:"profiles"`
	ActiveID string     `json:"activeId"`
}

// Store — потокобезопасное файловое хранилище профилей.
type Store struct {
	path string
	mu   sync.Mutex
	data storeData
}

// Load читает профили из файла (или создаёт пустое хранилище).
func Load(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "profiles.json")}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("profiles.json повреждён: %w", err)
	}
	return s, nil
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path) // атомарная замена
}

// List возвращает копию списка профилей.
func (s *Store) List() []*Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Profile, len(s.data.Profiles))
	copy(out, s.data.Profiles)
	return out
}

// ActiveID возвращает id активного профиля.
func (s *Store) ActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.ActiveID
}

// SetActive помечает профиль активным.
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.find(id) == nil {
		return fmt.Errorf("профиль %q не найден", id)
	}
	s.data.ActiveID = id
	return s.save()
}

// Get возвращает профиль по id.
func (s *Store) Get(id string) *Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.find(id)
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// AddManual создаёт ручной профиль из ссылок/JSON и сразу считает ноды.
// Если имя не задано — формируем его автоматически из тегов нод.
func (s *Store) AddManual(name, raw string) (*Profile, error) {
	nodes, err := config.DecodeSubscription([]byte(raw))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = deriveManualName(nodes)
	}
	p := &Profile{
		ID:        randomID(),
		Name:      name,
		Kind:      "manual",
		Raw:       raw,
		NodeCount: len(nodes),
		UpdatedAt: time.Now(),
	}
	return s.add(p)
}

// AddSubscription создаёт профиль-подписку и загружает её содержимое.
func (s *Store) AddSubscription(ctx context.Context, name, subURL string) (*Profile, error) {
	body, err := config.FetchSubscription(ctx, subURL)
	if err != nil {
		return nil, err
	}
	nodes, err := config.DecodeSubscription(body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = deriveSubName(subURL)
	}
	p := &Profile{
		ID:        randomID(),
		Name:      name,
		Kind:      "subscription",
		SubURL:    subURL,
		Raw:       string(body),
		NodeCount: len(nodes),
		UpdatedAt: time.Now(),
	}
	return s.add(p)
}

// deriveManualName формирует имя ручного профиля из тегов нод:
// «<тег первой ноды>» и «+N», если нод несколько.
func deriveManualName(nodes []config.Node) string {
	if len(nodes) == 0 {
		return "Профиль"
	}
	base := strings.TrimSpace(nodes[0].Tag)
	if base == "" {
		base = "Профиль"
	}
	if len(nodes) > 1 {
		base = fmt.Sprintf("%s +%d", base, len(nodes)-1)
	}
	return base
}

// deriveSubName берёт имя подписки из хоста её URL.
func deriveSubName(subURL string) string {
	if u, err := url.Parse(subURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "Подписка"
}

// Refresh перезагружает подписку с сервера.
func (s *Store) Refresh(ctx context.Context, id string) (*Profile, error) {
	s.mu.Lock()
	p := s.find(id)
	if p == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("профиль не найден")
	}
	if p.Kind != "subscription" || p.SubURL == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("профиль не является подпиской")
	}
	subURL := p.SubURL
	s.mu.Unlock()

	body, err := config.FetchSubscription(ctx, subURL)
	if err != nil {
		return nil, err
	}
	nodes, err := config.DecodeSubscription(body)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	p = s.find(id)
	if p == nil {
		return nil, fmt.Errorf("профиль удалён во время обновления")
	}
	p.Raw = string(body)
	p.NodeCount = len(nodes)
	p.UpdatedAt = time.Now()
	if err := s.save(); err != nil {
		return nil, err
	}
	cp := *p
	return &cp, nil
}

// Delete удаляет профиль.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data.Profiles {
		if p.ID == id {
			s.data.Profiles = append(s.data.Profiles[:i], s.data.Profiles[i+1:]...)
			if s.data.ActiveID == id {
				s.data.ActiveID = ""
			}
			return s.save()
		}
	}
	return fmt.Errorf("профиль %q не найден", id)
}

// ResolveNodes возвращает распарсенные ноды профиля.
func (s *Store) ResolveNodes(id string) ([]config.Node, error) {
	s.mu.Lock()
	p := s.find(id)
	if p == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("профиль %q не найден", id)
	}
	raw := p.Raw
	s.mu.Unlock()
	return config.DecodeSubscription([]byte(raw))
}

// --- внутреннее ---

func (s *Store) add(p *Profile) (*Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Profiles = append(s.data.Profiles, p)
	if s.data.ActiveID == "" {
		s.data.ActiveID = p.ID // первый профиль становится активным
	}
	if err := s.save(); err != nil {
		return nil, err
	}
	cp := *p
	return &cp, nil
}

func (s *Store) find(id string) *Profile {
	for _, p := range s.data.Profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
