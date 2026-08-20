//go:build android

package mobile

// Поверхность gomobile: всё, что видит Kotlin. gomobile умеет возить только строки, числа,
// []byte, error и связанные интерфейсы — поэтому сложные вещи ездят JSON-строками, как и
// на Windows, где Wails отдаёт то же самое в JS.

import (
	"os"
	"path/filepath"
	"sync"
)

// Platform — обязанности Kotlin-стороны. Всё, что требует Android API и чего у Go нет.
type Platform interface {
	// OpenTun получает параметры туннеля JSON-строкой, строит VpnService.Builder и
	// возвращает файловый дескриптор из establish().
	OpenTun(optionsJSON string) (int32, error)
	// ProtectFd выводит сокет ядра из-под VPN (VpnService.protect), иначе трафик к ноде
	// уйдёт в наш же туннель.
	ProtectFd(fd int32) error
	// Interfaces отдаёт список сетевых интерфейсов JSON-массивом. Своими силами Go его не
	// добудет: начиная с Android 11 SELinux запрещает приложению netlink-сокет
	// (`avc: denied { bind } ... netlink_route_socket`), и net.Interfaces() возвращает
	// ошибку — ядро в этот момент пишет «no available network interface» и никуда не
	// ходит. У Java-стороны свой путь через ioctl, он разрешён.
	Interfaces() (string, error)
	// WriteLog — строка журнала ядра; level в шкале sing-box (0 panic … 6 trace).
	WriteLog(level int32, message string)
}

var (
	pathsMu   sync.Mutex
	sBasePath string
	sTempPath string
)

// Setup задаёт рабочие каталоги. basePath — files-каталог приложения: туда лягут
// cache.db и скачанные наборы правил. Зовётся один раз при старте процесса.
func Setup(base string, temp string) error {
	if base == "" {
		return os.ErrInvalid
	}
	if temp == "" {
		temp = filepath.Join(base, "tmp")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(temp, 0o700); err != nil {
		return err
	}
	pathsMu.Lock()
	sBasePath = base
	sTempPath = temp
	pathsMu.Unlock()
	return nil
}

func basePath() string {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	return sBasePath
}

func tempPath() string {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	return sTempPath
}
