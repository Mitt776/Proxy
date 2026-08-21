//go:build android

package mobile

// Диспетчер вызовов из WebView. На Windows эту роль играет Wails: он сам биндит
// публичные методы App в JS. Здесь биндинга нет, поэтому имя метода и аргументы
// приезжают строками, а таблица ниже — ровно та же поверхность, что видит
// десктопный фронтенд.
//
// Методы, которых на Android нет, в таблицу не попадают, и фронтенд получает
// внятную ошибку вместо тишины:
//   - IsAdmin, GetAutostart/SetAutostart, SetMinimizeToTray — трея и реестра нет;
//   - PickCoreFile/SetCorePath/ResetCorePath — ядро вшито в APK;
//   - ListProcesses — правил по процессу на телефоне нет (см. план порта);
//   - CheckDomain — требует `sing-box rule-set match`, то есть ядра процессом;
//   - GetConnections/CloseConnection/CloseAllConnections — вкладки «Трафик» нет.

import (
	"encoding/json"
	"fmt"
	"runtime/debug"

	"Proxy/backend/appcore"
	"Proxy/backend/rules"
)

// handler разбирает аргументы и возвращает то, что уедет в JS.
type handler func(a *application, args json.RawMessage) (any, error)

var methods map[string]handler

func init() {
	methods = map[string]handler{
		// --- окружение и состояние ---
		"GetAppInfo":  func(a *application, _ json.RawMessage) (any, error) { return a.appInfo(), nil },
		"GetState":    func(a *application, _ json.RawMessage) (any, error) { return a.core.State(), nil },
		"GetStatus":   func(a *application, _ json.RawMessage) (any, error) { return a.core.GetStatus(), nil },
		"GetLogs":     func(a *application, _ json.RawMessage) (any, error) { return a.core.GetLogs(), nil },
		"GetLanguage": func(a *application, _ json.RawMessage) (any, error) { return a.core.CurrentLang(), nil },
		"SetLanguage": arg1(func(a *application, lang string) (any, error) {
			_, err := a.core.SetLanguage(lang)
			return nil, err
		}),

		// --- подключение ---
		// Connect на Windows принимает enableTUN; на Android перехват всегда
		// полный, поэтому аргумент игнорируется — фронтенд общий.
		"Connect":    func(a *application, _ json.RawMessage) (any, error) { return nil, a.Connect() },
		"Disconnect": func(a *application, _ json.RawMessage) (any, error) { return nil, a.Disconnect() },

		// --- профили ---
		"ListProfiles":       func(a *application, _ json.RawMessage) (any, error) { return a.core.ListProfiles(), nil },
		"GetActiveProfileID": func(a *application, _ json.RawMessage) (any, error) { return a.core.GetActiveProfileID(), nil },
		"AddManualProfile": arg2(func(a *application, name, raw string) (any, error) {
			return a.core.AddManualProfile(name, raw)
		}),
		"AddSubscriptionProfile": arg2(func(a *application, name, url string) (any, error) {
			return a.core.AddSubscriptionProfile(name, url)
		}),
		"RefreshProfile":   arg1(func(a *application, id string) (any, error) { return a.core.RefreshProfile(id) }),
		"DeleteProfile":    arg1(func(a *application, id string) (any, error) { return nil, a.core.DeleteProfile(id) }),
		"SetActiveProfile": arg1(func(a *application, id string) (any, error) { return nil, a.core.SetActiveProfile(id) }),
		"ListProfileNodes": arg1(func(a *application, id string) (any, error) { return a.core.ListProfileNodes(id) }),
		"ProfileConfigJSON": arg1(func(a *application, id string) (any, error) {
			return a.core.ProfileConfigJSON(id)
		}),
		"ProfileRaw": arg1(func(a *application, id string) (any, error) { return a.core.ProfileRaw(id) }),
		"ProfileQR":  arg1(func(a *application, id string) (any, error) { return a.core.ProfileQR(id) }),

		// --- маршрутизация ---
		"GetRouting": func(a *application, _ json.RawMessage) (any, error) { return a.core.GetRouting(), nil },
		"SetRouting": decode1(func(a *application, cfg rules.Config) (any, error) {
			return nil, a.core.SetRouting(cfg)
		}),
		"AddRule":    decode1(func(a *application, r rules.Rule) (any, error) { return a.core.AddRule(r) }),
		"UpdateRule": decode1(func(a *application, r rules.Rule) (any, error) { return nil, a.core.UpdateRule(r) }),
		"DeleteRule": arg1(func(a *application, id string) (any, error) { return nil, a.core.DeleteRule(id) }),
		"MoveRule": func(a *application, args json.RawMessage) (any, error) {
			var v struct {
				ID    string
				Index int
			}
			if err := decodeArgs(args, &v.ID, &v.Index); err != nil {
				return nil, err
			}
			return nil, a.core.MoveRule(v.ID, v.Index)
		},
		"SetRuleEnabled": func(a *application, args json.RawMessage) (any, error) {
			var id string
			var enabled bool
			if err := decodeArgs(args, &id, &enabled); err != nil {
				return nil, err
			}
			return nil, a.core.SetRuleEnabled(id, enabled)
		},
		"SetRoutingFinal": arg1(func(a *application, final string) (any, error) {
			return nil, a.core.SetRoutingFinal(final)
		}),
		"AddGroup":    decode1(func(a *application, g rules.Group) (any, error) { return a.core.AddGroup(g) }),
		"UpdateGroup": decode1(func(a *application, g rules.Group) (any, error) { return nil, a.core.UpdateGroup(g) }),
		"DeleteGroup": arg1(func(a *application, id string) (any, error) { return nil, a.core.DeleteGroup(id) }),

		// --- наборы правил ---
		"ListRuleSets": func(a *application, _ json.RawMessage) (any, error) { return a.core.ListRuleSets(), nil },
		"AddRuleSet":   decode1(func(a *application, rs rules.RuleSet) (any, error) { return a.core.AddRuleSet(rs) }),
		"UpdateRuleSet": decode1(func(a *application, rs rules.RuleSet) (any, error) {
			return nil, a.core.UpdateRuleSet(rs)
		}),
		"DeleteRuleSet":  arg1(func(a *application, id string) (any, error) { return nil, a.core.DeleteRuleSet(id) }),
		"RefreshRuleSet": arg1(func(a *application, id string) (any, error) { return a.core.RefreshRuleSet(id) }),

		// --- режим и ноды ---
		"GetMode":    func(a *application, _ json.RawMessage) (any, error) { return a.core.GetMode(), nil },
		"SetMode":    arg1(func(a *application, mode string) (any, error) { return nil, a.core.SetMode(mode) }),
		"GetProxies": func(a *application, _ json.RawMessage) (any, error) { return a.core.GetProxies() },
		"SelectNode": arg1(func(a *application, name string) (any, error) { return nil, a.core.SelectNode(name) }),
		"TestDelay":  arg1(func(a *application, name string) (any, error) { return a.core.TestDelay(name) }),
		"ExternalIP": func(a *application, _ json.RawMessage) (any, error) { return a.core.ExternalIP() },

		// --- настройки ---
		"GetSettings":  func(a *application, _ json.RawMessage) (any, error) { return a.core.GetSettings(), nil },
		"SetBlockQUIC": arg1b(func(a *application, block bool) (any, error) { return nil, a.core.SetBlockQUIC(block) }),
		"SetSubUpdateHours": arg1i(func(a *application, hours int) (any, error) {
			return nil, a.core.SetSubUpdateHours(hours)
		}),
		"GetLogLevel": func(a *application, _ json.RawMessage) (any, error) { return a.core.GetLogLevel(), nil },
		"SetLogLevel": arg1(func(a *application, level string) (any, error) { return nil, a.core.SetLogLevel(level) }),

		// --- приложения мимо VPN (только Android) ---
		"GetExcludedApps": func(a *application, _ json.RawMessage) (any, error) { return a.core.GetExcludedApps(), nil },
		"SetExcludedApps": decode1(func(a *application, pkgs []string) (any, error) {
			return nil, a.core.SetExcludedApps(pkgs)
		}),

		// --- обновления ---
		"CheckUpdate": func(a *application, _ json.RawMessage) (any, error) { return a.core.CheckUpdate() },
		"SetUpdateCheck": arg1b(func(a *application, on bool) (any, error) {
			return nil, a.core.SetUpdateCheck(on)
		}),
	}
}

// dispatch выполняет вызов. Неизвестный метод — это либо десктопный метод в общем
// компоненте, либо опечатка; в обоих случаях молчать нельзя.
func dispatch(a *application, method string, argsJSON string) (result any, err error) {
	// Паника внутри вызова не должна убивать процесс: ядро работает библиотекой
	// в нём же, и вместе с приложением молча оборвётся туннель — у пользователя
	// это выглядит как «интернет пропал», причём в самый неудачный момент.
	// Разбор чужого ввода (подписка, ссылка, QR) идёт по этим же путям, поэтому
	// цена ошибки в парсере — не диагностика, а отвал связи.
	defer func() {
		if r := recover(); r != nil {
			err = appcore.CodedErrf(appcore.ErrInternal, "внутренняя ошибка в %s: %v", method, r)
			result = nil
			// Ядро — единственный путь строки в журнал, но обработчик паники сам
			// падать не имеет права: проверка дешевле разбирательства.
			if a != nil && a.core != nil {
				a.core.LogLine(fmt.Sprintf("паника в %s: %v\n%s", method, r, debug.Stack()))
			}
		}
	}()

	h, ok := methods[method]
	if !ok {
		return nil, appcore.CodedErrf(appcore.ErrNoMethod, "метод %q на Android не поддерживается", method)
	}
	var args json.RawMessage
	if argsJSON != "" {
		args = json.RawMessage(argsJSON)
	}
	return h(a, args)
}

// --- разбор аргументов ---
//
// Аргументы всегда приезжают JSON-массивом — так их отдаёт мост, повторяя вызов
// сгенерированного Wails биндинга.

func decodeArgs(args json.RawMessage, targets ...any) error {
	if len(args) == 0 {
		if len(targets) == 0 {
			return nil
		}
		return appcore.CodedErr(appcore.ErrBadArgs, "аргументы не переданы")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return appcore.CodedErrf(appcore.ErrBadArgs, "аргументы не разобрались: %w", err)
	}
	if len(raw) < len(targets) {
		return appcore.CodedErrf(appcore.ErrBadArgs, "ожидалось %d аргументов, пришло %d", len(targets), len(raw))
	}
	for i, target := range targets {
		if err := json.Unmarshal(raw[i], target); err != nil {
			return appcore.CodedErrf(appcore.ErrBadArgs, "аргумент %d: %w", i+1, err)
		}
	}
	return nil
}

func arg1(fn func(*application, string) (any, error)) handler {
	return func(a *application, args json.RawMessage) (any, error) {
		var s string
		if err := decodeArgs(args, &s); err != nil {
			return nil, err
		}
		return fn(a, s)
	}
}

func arg2(fn func(*application, string, string) (any, error)) handler {
	return func(a *application, args json.RawMessage) (any, error) {
		var first, second string
		if err := decodeArgs(args, &first, &second); err != nil {
			return nil, err
		}
		return fn(a, first, second)
	}
}

func arg1b(fn func(*application, bool) (any, error)) handler {
	return func(a *application, args json.RawMessage) (any, error) {
		var v bool
		if err := decodeArgs(args, &v); err != nil {
			return nil, err
		}
		return fn(a, v)
	}
}

func arg1i(fn func(*application, int) (any, error)) handler {
	return func(a *application, args json.RawMessage) (any, error) {
		var v int
		if err := decodeArgs(args, &v); err != nil {
			return nil, err
		}
		return fn(a, v)
	}
}

// decode1 — первый аргумент как структура.
func decode1[T any](fn func(*application, T) (any, error)) handler {
	return func(a *application, args json.RawMessage) (any, error) {
		var v T
		if err := decodeArgs(args, &v); err != nil {
			return nil, err
		}
		return fn(a, v)
	}
}
