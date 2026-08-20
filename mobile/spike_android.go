//go:build android

package mobile

// Времянка этапа 0: собрать конфиг из одной ссылки на ноду, чтобы проверить связку
// парсер → генератор → ядро → VpnService на живом трафике. На этапе 1 её заменит
// полноценный dispatch поверх профилей и правил, и файл уедет.

import (
	"crypto/rand"
	"encoding/hex"

	"Proxy/backend/config"
	"Proxy/backend/rules"
)

// SpikeConfig собирает конфиг для одной ноды с включённым TUN.
// Ссылка приезжает из интерфейса и нигде не сохраняется — в коде её быть не должно.
func SpikeConfig(link string) (string, error) {
	node, err := config.ParseLink(link)
	if err != nil {
		return "", err
	}

	secret := make([]byte, 16)
	if _, err = rand.Read(secret); err != nil {
		return "", err
	}

	data, err := config.Generate(config.Options{
		MixedPort:    2080,
		ClashAPIPort: 9090,
		ClashSecret:  hex.EncodeToString(secret),
		LogLevel:     "info",
		EnableTUN:    true,
		TUNStack:     "gvisor",
		Nodes:        []config.Node{node},
		Routing:      rules.Default(),
		BlockQUIC:    true,
		RuleSetDir:   basePath(),
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}
