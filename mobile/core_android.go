//go:build android

package mobile

// Сборка и запуск sing-box как библиотеки. На Windows этим занимается
// backend/core/manager.go: запускает sing-box.exe процессом и следит за ним. Здесь
// ядро живёт внутри нашего процесса, поэтому «запустить» означает собрать box и
// позвать Start.

import (
	"context"
	"os"
	"sync"

	singbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/filemanager"
)

// baseContext собирает контекст с полным реестром протоколов — тот же include,
// который использует и штатный sing-box, и libbox.
func baseContext(platform adapter.PlatformInterface) context.Context {
	ctx := context.Background()
	ctx = filemanager.WithDefault(ctx, basePath(), tempPath(), os.Getuid(), os.Getgid())
	ctx = singbox.Context(
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

// checkConfig — прямой аналог `sing-box check` на Windows: конфиг проверяется до
// того, как им заменят рабочий. Без этого битое правило гасило бы живое соединение.
//
// Платформенный слой обязателен даже здесь, хотя проверка ничего не запускает: без
// него ядро при сборке box заводит собственный монитор сети и падает с
// «netlink socket in Android is banned by Google». Отсюда заглушка — туннель она
// открыть не может, но до этого дело и не доходит.
func checkConfig(configContent string) error {
	platform := &platformImpl{kt: checkStub{}}
	ctx := baseContext(platform)
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(configContent))
	if err != nil {
		return E.Cause(err, "decode config")
	}
	instance, err := singbox.New(singbox.Options{Context: ctx, Options: options})
	if err != nil {
		return err
	}
	return instance.Close()
}

// checkStub — платформа для проверки конфига. Ядро её ни о чём не спрашивает:
// box создаётся и сразу закрывается, не доходя до открытия туннеля.
type checkStub struct{}

func (checkStub) OpenTun(string) (int32, error) {
	return 0, E.New("tun is not available during config check")
}
func (checkStub) ProtectFd(int32) error       { return nil }
func (checkStub) Interfaces() (string, error) { return "[]", nil }
func (checkStub) WriteLog(int32, string)      {}

func (checkStub) FindConnectionOwner(int32, string, int32, string, int32) (string, error) {
	return "{}", nil
}

// newInstance поднимает ядро на переданном конфиге. Журнал ядра уходит в onLog —
// оттуда он попадает и в кольцевой буфер вкладки «Журнал», и в logcat.
func newInstance(configContent string, platform Platform, onLog func(string)) (*singbox.Box, error) {
	if platform == nil {
		return nil, E.New("platform interface is required")
	}

	platformImplementation := &platformImpl{kt: platform}
	// Ставим до сборки box: ядро дёрнет NetworkInterfaces уже во время Start, а
	// Kotlin к этому моменту мог прислать сеть.
	platformMu.Lock()
	currentPlatform = platformImplementation
	platformMu.Unlock()

	ctx := baseContext(platformImplementation)

	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(configContent))
	if err != nil {
		clearPlatform(platformImplementation)
		return nil, E.Cause(err, "decode config")
	}

	instance, err := singbox.New(singbox.Options{
		Context:           ctx,
		Options:           options,
		PlatformLogWriter: &platformLogWriter{platform: platformImplementation, onLog: onLog},
	})
	if err != nil {
		clearPlatform(platformImplementation)
		return nil, E.Cause(err, "create service")
	}
	if err = instance.Start(); err != nil {
		instance.Close()
		clearPlatform(platformImplementation)
		return nil, E.Cause(err, "start service")
	}

	// Монитор существует только после сборки box — теперь можно донести до ядра
	// сеть, о которой Kotlin сообщил раньше.
	platformImplementation.applyPending()
	return instance, nil
}

var (
	platformMu      sync.Mutex
	currentPlatform *platformImpl
)

func clearPlatform(platformImplementation *platformImpl) {
	platformMu.Lock()
	if currentPlatform == platformImplementation {
		currentPlatform = nil
	}
	platformMu.Unlock()
}

// UpdateDefaultInterface зовётся из Kotlin по ConnectivityManager: index == -1 —
// сети нет. Может прийти раньше, чем ядро создаст монитор, — тогда значение просто
// запоминается.
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

// platformLogWriter отдаёт журнал ядра в Kotlin (там он попадёт в logcat) и в наш
// кольцевой буфер для вкладки «Журнал».
type platformLogWriter struct {
	platform *platformImpl
	onLog    func(string)
}

func (w *platformLogWriter) WriteMessage(level log.Level, message string) {
	// Цвет ядро ставит всегда — см. stripANSI.
	message = stripANSI(message)
	w.platform.kt.WriteLog(int32(level), message)
	if w.onLog != nil {
		w.onLog(message)
	}
}
