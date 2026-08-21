//go:build android

package mobile

// Поверхность gomobile: всё, что видит Kotlin. gomobile умеет возить только
// строки, числа, []byte, error и связанные интерфейсы — поэтому сложные вещи ездят
// JSON-строками, ровно как Wails возит их в JS на Windows.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"Proxy/backend/appcore"
	"Proxy/backend/core"
	C "github.com/sagernet/sing-box/constant"
)

// Platform — обязанности VpnService. Всё, что требует Android API и чего у Go нет.
type Platform interface {
	// OpenTun получает параметры туннеля JSON-строкой, строит VpnService.Builder и
	// возвращает файловый дескриптор из establish().
	OpenTun(optionsJSON string) (int32, error)
	// ProtectFd выводит сокет ядра из-под VPN (VpnService.protect), иначе трафик к
	// ноде уйдёт в наш же туннель.
	ProtectFd(fd int32) error
	// Interfaces отдаёт список сетевых интерфейсов JSON-массивом. Своими силами Go
	// его не добудет: начиная с Android 11 SELinux запрещает приложению
	// netlink-сокет (`avc: denied { bind } ... netlink_route_socket`), и
	// net.Interfaces() возвращает ошибку — ядро в этот момент пишет
	// «no available network interface» и никуда не ходит. У Java-стороны свой путь
	// через ioctl, он разрешён.
	Interfaces() (string, error)
	// FindConnectionOwner отвечает, какое приложение открыло соединение, — JSON
	// вида {"uid":10566,"package":"com.example"}; пустой package означает
	// «не определилось».
	//
	// Реализовать обязательно, даже если результат нам не нужен: ядро на Android
	// включает поиск процесса всегда, когда есть платформенный слой, и без этого
	// метода лезет в netlink, который Android запрещает — в журнал сыплется
	// «failed to search process: dial netlink: permission denied» на каждом
	// соединении.
	FindConnectionOwner(ipProtocol int32, sourceAddress string, sourcePort int32, destinationAddress string, destinationPort int32) (string, error)
	// WriteLog — строка журнала ядра; level в шкале sing-box (0 panic … 6 trace).
	WriteLog(level int32, message string)
}

// Controller — то, что умеет только Kotlin: поднять и погасить VpnService.
// Ядро без него не запустить — TUN выдаёт сервис, а не мы.
type Controller interface {
	// StartTunnel поднимает foreground-сервис с этим конфигом. Возвращается сразу:
	// сам сервис потом позовёт ServiceStart.
	StartTunnel(configJSON string) error
	// StopTunnel гасит сервис; тот в свою очередь зовёт ServiceStop.
	StopTunnel()
}

// EventSink — канал сообщений в приложение. OnEvent повторяет события Wails
// (core:state, core:log, core:stats, profiles:changed), остальное — для сервиса и
// уведомления, которым разбирать JSON незачем.
type EventSink interface {
	// OnEvent — событие для WebView; payloadJSON скармливается JSON.parse.
	OnEvent(name string, payloadJSON string)
	// OnResult — ответ на Call с тем же id. Ровно одно из полей непустое.
	OnResult(id int32, resultJSON string, errText string)
	// OnState — состояние ядра для сервиса и уведомления.
	OnState(state string, reason string)
	// OnSpeed — скорость вверх/вниз в байтах в секунду.
	OnSpeed(downSpeed int64, upSpeed int64)
}

var (
	pathsMu   sync.Mutex
	sBasePath string
	sTempPath string
)

// setupPaths задаёт рабочие каталоги. base — files-каталог приложения: туда лягут
// profiles.json, settings.json, routing.json, cache.db и скачанные наборы правил.
func setupPaths(base string, temp string) error {
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

// Start поднимает приложение: читает хранилища и подключает ядро. Зовётся один раз
// на процесс, до первого Call. Повторный вызов безвреден.
//
// assetsDir — каталог с .srs, распакованными из APK; sysLang — язык системы
// (пусто = русский по умолчанию), он используется, пока пользователь не выбрал
// язык сам.
func Start(dataDir string, assetsDir string, sysLang string, sink EventSink, controller Controller) error {
	if sink == nil || controller == nil {
		return appcore.CodedErr(appcore.ErrNotReady, "мост не готов")
	}
	if err := setupPaths(dataDir, ""); err != nil {
		return err
	}

	appMu.Lock()
	defer appMu.Unlock()
	if app != nil {
		return nil
	}

	instance := &application{sink: sink, sysLang: appcore.NormalizeLang(sysLang)}
	if instance.sysLang == "" {
		instance.sysLang = "ru"
	}
	// core.ResolvePaths ищет ядро рядом с exe — на Android искать нечего, ядро
	// вшито в APK, а каталоги приносит Kotlin.
	instance.core = appcore.New(appcore.Options{
		Host:  instance,
		Paths: &core.Paths{DataDir: dataDir, AssetsDir: assetsDir},
	})
	for _, issue := range instance.core.Load() {
		// Повреждённый файл отложен в *.bad, хранилище рабочее — сообщаем и живём
		// дальше, как и на Windows.
		instance.sink.OnEvent("state:issue", fmt.Sprintf(`{"kind":%q,"error":%q}`, issue.Kind, issue.Err.Error()))
	}

	instance.runner = newRunner(controller)
	instance.runner.onState = instance.onCoreState
	instance.runner.onLog = instance.onCoreLog
	instance.core.SetRunner(instance.runner)
	instance.core.StartSubScheduler()

	app = instance
	return nil
}

// Call выполняет метод приложения. Асинхронно и намеренно: синхронный мост
// заблокировал бы поток WebView на обновлении подписки или тесте задержки, а это
// секунды с замершим интерфейсом. Ответ приходит в EventSink.OnResult с тем же id
// — так же, как Wails резолвит промис на Windows.
func Call(id int32, method string, argsJSON string) {
	appMu.Lock()
	instance := app
	appMu.Unlock()

	if instance == nil {
		return
	}
	go func() {
		result, err := dispatch(instance, method, argsJSON)
		if err != nil {
			instance.sink.OnResult(id, "", err.Error())
			return
		}
		data, merr := marshalResult(result)
		if merr != nil {
			instance.sink.OnResult(id, "", merr.Error())
			return
		}
		instance.sink.OnResult(id, data, "")
	}()
}

// ServiceStart зовётся из TunnelService, когда VpnService готов выдать TUN.
// Пустой configJSON означает «взять тот, что подготовил Connect».
func ServiceStart(configJSON string, platform Platform) error {
	instance, err := current()
	if err != nil {
		return err
	}
	return instance.runner.serviceStart([]byte(configJSON), platform)
}

// ServiceStop зовётся при остановке сервиса — в том числе когда туннель погасила
// сама система (отзыв разрешения, always-on другого приложения).
func ServiceStop() {
	appMu.Lock()
	instance := app
	appMu.Unlock()
	if instance == nil {
		return
	}
	instance.runner.serviceStop()
}

// NotificationInfo — данные для уведомления сервиса: язык интерфейса и имя
// активного профиля. Уведомление живёт вне WebView, поэтому переводить его
// словарями фронтенда нечем — язык едет сюда, а строки лежат таблицей в Kotlin.
//
// Зовётся на каждой перерисовке уведомления (раз в секунду при живом трафике),
// поэтому только локальное состояние: ни одного обращения к Clash API.
func NotificationInfo() string {
	appMu.Lock()
	instance := app
	appMu.Unlock()
	if instance == nil {
		return "{}"
	}

	name := ""
	activeID := instance.core.GetActiveProfileID()
	for _, p := range instance.core.ListProfiles() {
		if p.ID == activeID {
			name = p.Name
			break
		}
	}
	data, err := json.Marshal(struct {
		Lang    string `json:"lang"`
		Profile string `json:"profile"`
		Since   int64  `json:"since"`
	}{instance.core.CurrentLang(), name, instance.core.ConnectedAt()})
	if err != nil {
		return "{}"
	}
	return string(data)
}

// IsRunning — состояние для холодного старта Activity.
func IsRunning() bool {
	appMu.Lock()
	instance := app
	appMu.Unlock()
	return instance != nil && instance.core.State() == "running"
}

// Shutdown гасит фоновые горутины. Ядро при этом не трогаем: сервис может жить
// дальше, даже когда окна уже нет.
func Shutdown() {
	appMu.Lock()
	instance := app
	appMu.Unlock()
	if instance != nil {
		instance.core.Close()
	}
}

func current() (*application, error) {
	appMu.Lock()
	instance := app
	appMu.Unlock()
	if instance == nil {
		return nil, appcore.CodedErr(appcore.ErrNotReady, "приложение не инициализировано")
	}
	return instance, nil
}

// coreVersion — версия вшитого ядра для раздела «О программе».
func coreVersion() string { return C.Version }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// marshalResult превращает результат метода в JSON для JS. nil едет как null —
// на той стороне это undefined-подобное значение, которого и ждёт фронтенд от
// методов без возврата.
func marshalResult(result any) (string, error) {
	if result == nil {
		return "null", nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", appcore.CodedErrf(appcore.ErrBadArgs, "ответ не сериализуется: %w", err)
	}
	return string(data), nil
}
