// Package appcore — переносимое ядро приложения: профили, маршрутизация, настройки,
// Clash API и подписки. Не знает ни о Wails, ни о Windows, ни об Android — всё
// платформенное приходит через Host и Runner. Один и тот же код обслуживает GUI на
// Windows и APK: иначе withRouting, applyRouting и планировщик подписок пришлось бы
// держать в двух копиях, и разошлись бы они молча.
package appcore

import "fmt"

// Коды ошибок для двуязычного интерфейса.
//
// Wails отдаёт `error` во фронтенд обычной строкой — ни типа, ни поля с кодом там
// не остаётся. Поэтому код едет префиксом в самом тексте: `[E_NO_PROFILE] не выбран
// активный профиль`. Фронтенд (lib/i18n/errors.ts) вырезает префикс и подставляет
// перевод, а при неизвестном коде показывает остаток строки как есть — так вывод
// самого sing-box и редкие внутренние ошибки не теряются.
//
// Кодами размечены только те ошибки, которые реально видит пользователь; всё
// остальное (ошибки файловой системы, сети, разбора) уходит наверх как есть.
const (
	ErrNotReady        = "E_NOT_READY"         // хранилище/приложение ещё не инициализировано
	ErrNoProfile       = "E_NO_PROFILE"        // не выбран активный профиль
	ErrProfileNotFound = "E_PROFILE_NOT_FOUND" // профиль с таким id не найден
	ErrTUNRights       = "E_TUN_RIGHTS"        // пользователь отклонил UAC для TUN
	ErrElevate         = "E_ELEVATE"           // повышение прав не удалось по другой причине
	ErrModeUnknown     = "E_MODE_UNKNOWN"      // неизвестный режим маршрутизации
	ErrCoreRunning     = "E_CORE_RUNNING"      // действие требует остановленного ядра
	ErrCoreStopped     = "E_CORE_STOPPED"      // действие требует работающего ядра
	ErrCoreInvalid     = "E_CORE_INVALID"      // выбранный файл не является sing-box
	ErrCoreCheck       = "E_CORE_CHECK"        // ядро отвергло сгенерированный конфиг
	ErrClashNotReady   = "E_CLASH_NOT_READY"   // Clash API недоступен
	ErrNoSelector      = "E_NO_SELECTOR"       // в конфиге нет селектора нод
	ErrIPUnknown       = "E_IP_UNKNOWN"        // внешний IP определить не удалось
	ErrQRNothing       = "E_QR_NOTHING"        // профиль пуст — нечего кодировать
	ErrQRGenerate      = "E_QR_GENERATE"       // не удалось построить QR
	ErrQRImage         = "E_QR_IMAGE"          // картинка не читается
	ErrQRNotFound      = "E_QR_NOTFOUND"       // на картинке нет QR-кода
	ErrLangUnknown     = "E_LANG_UNKNOWN"      // запрошен неподдерживаемый язык

	// Ошибки моста в WebView на Android. На Windows их нет: там методы биндит
	// Wails, и несуществующий метод не собрался бы во фронтенде.
	ErrNoMethod = "E_NO_METHOD" // метода нет в мобильной сборке
	ErrBadArgs  = "E_BAD_ARGS"  // аргументы вызова не разобрались
	ErrInternal = "E_INTERNAL"  // паника внутри вызова, перехваченная мостом

	// ErrInsecureURL — источник запрошен по http. Отдельным кодом, потому что это
	// не «не скачалось», а сознательный отказ доверять открытому каналу: текст
	// должен объяснять причину, иначе выглядит как поломка.
	ErrInsecureURL = "E_INSECURE_URL"

	ErrSetNotFound = "E_SET_NOT_FOUND" // набор правил не найден по ID
	ErrSetNotList  = "E_SET_NOT_LIST"  // операция только для текстовых списков (.lst)
	ErrSetFetch    = "E_SET_FETCH"     // список правил не скачался или оказался пустым

	ErrUpdateCheck = "E_UPDATE_CHECK" // не удалось спросить GitHub о новой версии
)

// CodedErr собирает ошибку с кодом. Русский текст остаётся фолбэком на случай,
// если фронтенд не знает код (старая сборка UI, неожиданный путь).
func CodedErr(code, ru string) error {
	return fmt.Errorf("[%s] %s", code, ru)
}

// CodedErrf — то же для форматируемых сообщений, включая обёртку через %w.
func CodedErrf(code, format string, args ...any) error {
	return fmt.Errorf("["+code+"] "+format, args...)
}
