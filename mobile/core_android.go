//go:build android

package mobile

// Жизненный цикл ядра. На Windows этим занимается backend/core/manager.go: запускает
// sing-box.exe процессом и следит за ним. Здесь ядро — библиотека в нашем же процессе,
// поэтому «запустить» означает собрать box и позвать Start.

import (
	"context"
	"os"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/filemanager"
)

var (
	instanceMu sync.Mutex
	instance   *box.Box

	// Отдельный мьютекс: UpdateDefaultInterface прилетает из колбэка ConnectivityManager
	// и может совпасть по времени со Start, который держит instanceMu всё время сборки
	// конфига. На общем мьютексе это был бы дедлок.
	platformMu      sync.Mutex
	currentPlatform *platformImpl
)

// baseContext собирает контекст с полным реестром протоколов — тот же include, который
// использует и штатный sing-box, и libbox.
func baseContext(platform adapter.PlatformInterface) context.Context {
	ctx := context.Background()
	ctx = filemanager.WithDefault(ctx, basePath(), tempPath(), os.Getuid(), os.Getgid())
	ctx = box.Context(
		ctx,
		include.InboundRegistry(),
		include.OutboundRegistry(),
		include.EndpointRegistry(),
		include.DNSTransportRegistry(),
		include.ServiceRegistry(),
		include.CertificateProviderRegistry(),
	)
	if platform != nil {
		service.MustRegister[adapter.PlatformInterface](ctx, platform)
	}
	return ctx
}

// CheckConfig — прямой аналог `sing-box check` на Windows: конфиг проверяется до того, как
// им заменят рабочий. Без этого битое правило гасило бы живое соединение.
func CheckConfig(configContent string) error {
	ctx := baseContext(nil)
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(configContent))
	if err != nil {
		return E.Cause(err, "decode config")
	}
	instance, err := box.New(box.Options{
		Context: ctx,
		Options: options,
	})
	if err != nil {
		return err
	}
	return instance.Close()
}

// Start поднимает ядро на переданном конфиге. Повторный вызов при живом ядре — ошибка:
// перезапуск идёт через Stop, иначе два box подрались бы за один TUN.
func Start(configContent string, platform Platform) error {
	if platform == nil {
		return E.New("platform interface is required")
	}

	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instance != nil {
		return E.New("core is already running")
	}

	platformImplementation := &platformImpl{kt: platform}
	// Ставим до сборки box: ядро дёрнет NetworkInterfaces уже во время Start, а Kotlin к
	// этому моменту мог прислать сеть.
	platformMu.Lock()
	currentPlatform = platformImplementation
	platformMu.Unlock()

	ctx := baseContext(platformImplementation)

	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(configContent))
	if err != nil {
		return E.Cause(err, "decode config")
	}

	newInstance, err := box.New(box.Options{
		Context:           ctx,
		Options:           options,
		PlatformLogWriter: (*platformLogWriter)(platformImplementation),
	})
	if err != nil {
		clearPlatform(platformImplementation)
		return E.Cause(err, "create service")
	}
	if err = newInstance.Start(); err != nil {
		newInstance.Close()
		clearPlatform(platformImplementation)
		return E.Cause(err, "start service")
	}

	instance = newInstance
	// Монитор существует только после box.New — теперь можно донести до ядра сеть,
	// о которой Kotlin сообщил раньше.
	platformImplementation.applyPending()
	return nil
}

func clearPlatform(platformImplementation *platformImpl) {
	platformMu.Lock()
	if currentPlatform == platformImplementation {
		currentPlatform = nil
	}
	platformMu.Unlock()
}

// Stop гасит ядро. Идемпотентен: остановка уже остановленного — не ошибка, иначе гонка
// между кнопкой «Отключить» и смертью сервиса давала бы ложную ошибку в интерфейсе.
func Stop() error {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instance == nil {
		return nil
	}
	err := instance.Close()
	instance = nil
	platformMu.Lock()
	currentPlatform = nil
	platformMu.Unlock()
	return err
}

// IsRunning — состояние для UI после холодного старта Activity.
func IsRunning() bool {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	return instance != nil
}

// UpdateDefaultInterface зовётся из Kotlin по ConnectivityManager: index == -1 — сети нет.
// Может прийти раньше, чем ядро создаст монитор, — тогда значение просто запоминается.
func UpdateDefaultInterface(name string, index int32) {
	platformMu.Lock()
	platformImplementation := currentPlatform
	platformMu.Unlock()
	if platformImplementation == nil {
		return
	}

	platformImplementation.mu.Lock()
	platformImplementation.pendingName = name
	platformImplementation.pendingIndex = index
	platformImplementation.hasPending = true
	monitor := platformImplementation.monitor
	platformImplementation.mu.Unlock()

	if monitor == nil {
		return
	}
	monitor.update(name, index)
}

// platformLogWriter отдаёт журнал ядра в Kotlin — там он попадёт и в logcat, и в наш
// кольцевой буфер для вкладки «Журнал».
type platformLogWriter platformImpl

func (w *platformLogWriter) WriteMessage(level log.Level, message string) {
	(*platformImpl)(w).kt.WriteLog(int32(level), message)
}
