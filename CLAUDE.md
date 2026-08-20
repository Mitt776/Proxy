# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## О проекте

**MitM** — портативный прокси-клиент для Windows: GUI на Wails v2 (Go + WebView2 + Svelte/TS),
сетевое ядро — **внешний процесс** `sing-box.exe`, которым управляем через сгенерированный
`config.json` и Clash API. Ядро в репозитории не хранится (см. `.gitignore`). Комментарии в коде —
на русском, интерфейс — двуязычный (RU/EN, см. «Двуязычность»).

Модуль Go по-прежнему называется `Proxy` — это внутреннее имя в путях импорта всех файлов, к
названию приложения отношения не имеет и переименованию не подлежит.

**Версия приложения бампится в двух местах сразу:** `const AppVersion` в [app.go](app.go) (его
читают UI и трей) и блок `info.productVersion` в [wails.json](wails.json) (ресурс версии в exe).
Разъехавшиеся значения = «В программе 2.0.0, в свойствах файла 1.3.1».

## Команды

```powershell
wails build                 # релизная сборка → build\bin\MitM.exe
wails dev                   # dev-режим с hot-reload фронтенда
go test ./...               # юнит-тесты (парсер ссылок, генератор, стор, system proxy)
go test ./backend/config -run TestXHTTPParsing -v   # одиночный тест
cd frontend; npm run check  # svelte-check по TS
```

`go` и `wails` есть в PATH (`C:\Program Files\Go\bin`, `%USERPROFILE%\go\bin`).

### Интеграционные тесты (тег `coretest`)

Требуют реальный `sing-box.exe` и настоящую ноду; ссылки на ноды **никогда не хранить в коде** —
только через env.

```powershell
$env:PROXY_ASSETS="D:\Project VSC\Proxy\assets"   # каталог с ядром/гео-базами
$env:PROXY_TEST_LINK="vless://..."                # реальная нода
go test -tags coretest ./backend/core -run TestRealNodeProxying -v
```

Тесты под тегом: `TestCoreEndToEnd` (integration_test.go), `TestRealNodeProxying`
(realnode_test.go), `TestCoreOverrideRunsXHTTP` (override_test.go, дополнительно `PROXY_FORK_CORE`),
job-object (job_windows_test.go), перезапуск и переключение режима на живом ядре
(core/restart_test.go), соединения и `rule-set match` (core/observe_test.go), проверка домена на
настоящем ядре (config/check_core_test.go), валидация правил **обоими ядрами** из `assets`
(config/validate_rules_test.go, там же `TestListRuleSetValidates` — набор из `.lst` и его
семантика глазами ядра).

Отдельно `config/tlsfragment_core_test.go` (`TestTLSFragmentActuallyFragments`) поднимает ядро с
фейковым сервером на loopback и смотрит, какими кусками до него доехал настоящий ClientHello:
`check` доказывает только то, что поле принято, а не то, что оно что-то делает.

Любое новое поле конфига обязано проехать через `config/validate_rules_test.go`: документации
sing-box верить нельзя, только вывод `check` штатного ядра **и** форка с XHTTP.

### Портативный релиз

`wails build` не копирует ассеты — после сборки вручную положить `assets\*.exe`, `*.dll`, `*.srs`
в `build\bin\assets\` и вычистить `build\bin\data\`.

Туда же кладётся [build/README-portable.txt](build/README-portable.txt) под именем `README.txt`:
раньше он жил только внутри `build\bin\` (каталог в .gitignore), поэтому благополучно отстал от
приложения на две версии и продолжал советовать запускать `Proxy.exe`.

## Архитектура

**Слои.** Фронтенд (`frontend/src/App.svelte` — только раскладка и переключение разделов; вся
логика в `frontend/src/lib/`) → биндинги Wails
(`frontend/wailsjs/`, генерируются, в git не хранятся) → `App` в [app.go](app.go) — единственный
объект, забинденный в JS; все публичные методы `App` = API для UI (наблюдаемость 1.3.0 —
в [observe.go](observe.go), там же соединения, проверка домена и наборы правил). Ниже —
`backend/*` пакеты,
не знающие ни о Wails, ни друг о друге, кроме `profile → config → rules`.

**Состав фронтенда (2.0.0).**

- `lib/shell/` — оболочка: `Splash` (заставка с логотипом и FLIP-перелётом знака в шапку),
  `TitleBar` (своя шапка безрамочного окна, `--wails-draggable`, переключатель RU/EN),
  `Sidebar` (плавающая колонка разделов), `TabHead` (шапка раздела), `LogoMark`, `tabs.ts`.
- `lib/connect/` — экран «Подключение»: `ConnectScreen`, `PowerButton` (3D-кнопка), `Flag`
  (вшитый набор SVG-флагов), `StatCard` + `Sparkline`, `ServerPicker`.
- `lib/` — разделы: `ProfilesTab`, `RulesTab` (+ `RuleEditor`, `ProcessPicker`, `DomainCheck`),
  `ConnectionsTab` (вкладка «Трафик»), `LogsTab`, `SettingsTab`.
- `lib/store.ts` — единственная подписка на `core:state`/`core:stats` и кольцевые буферы истории
  для спарклайнов. Буферы обязаны жить здесь: вкладки монтируются через `{#if}` и локальное
  состояние умирало бы при каждом переключении.
- `lib/ui.css` — токены оформления и общие классы (`.panel`, `.card`, `.btn`, `.icon-btn`, `.fld`,
  `.pill`, `.toggle`, `.check`, `.segmented`, `.modal-*`, `.tab-wrap`/`.tab-head`, `.rows`/`.row-i`).
  **Цвета в компонентах — только токенами.** Хардкод вроде `#1f6feb` когда-то расползся по десятку
  файлов, и перекрасить тему было нечем.
- `lib/icons/` — рукописный набор inline-SVG; библиотеку иконок не тянем.

**Поток подключения:** `App.Connect(enableTUN)` → `store.ResolveNodes` → `config.Generate(Options)`
→ `core.Manager.Check(json)` (`sing-box check` до старта) → `core.Manager.Start(json)` → запись
`data\config.json` → `sing-box.exe run -c ... -D data` → при `!enableTUN` ещё и
`system.SystemProxy.Set(127.0.0.1:2080)`.

**Пакеты:**

- [backend/rules/](backend/rules/) — модель маршрутизации: упорядоченный список правил и группы
  нод в `data\routing.json` (`rules.go` — типы и валидация, `ruleset.go` — описания
  удалённых наборов, `store.go` — хранилище, дефолты и `Migrate` со старых настроек).
  Листовой пакет: не знает ни о sing-box, ни о Wails, перевод
  правил в схему ядра живёт в `config/route.go`.
- [backend/config/](backend/config/) — разбор входа и генерация конфига ядра.
  `parser.go`/`parser_protocols.go` — ссылки `vless://` и др. в outbound-JSON; `clash.go` —
  Clash YAML; `subscription.go` — автоопределение формата (sing-box JSON / Clash / base64 / список
  ссылок); `generator.go` — сборка полного sing-box-конфига (DNS, route, inbounds, outbounds);
  `route.go` — правила `backend/rules` → route-правила sing-box; `check.go` — статическая
  проверка домена по правилам (матчинг делегирован ядру). Ключевые теги outbound-ов:
  `proxy` (selector), `auto` (urltest), `direct`; группы нод дают свои selector/urltest с тегом
  по имени группы.
- [backend/core/](backend/core/) — жизненный цикл ядра. `manager.go` — старт/стоп/`Check`/
  `Restart`, кольцевой буфер логов, колбэки `OnLog`/`OnState`; `paths.go` — резолв assets/data
  (портативность); `clashapi.go` — HTTP-клиент Clash API (список нод, выбор, задержки, трафик,
  режим, соединения и их обрыв); `proc_windows.go` — job object, убивающий ядро вместе с GUI.
  `Manager.RuleSetMatch` — обёртка над `sing-box rule-set match` для проверки домена.
- [backend/profile/](backend/profile/) — профили (ручные и подписки) в `data\profiles.json`.
- [backend/settings/](backend/settings/) — `data\settings.json`; при добавлении поля помни, что
  zero-value применяется к старым файлам (см. инверсию `AllowQUIC`). Поля `routingMode`,
  `blockAds` и три списка доменов — наследие до 1.2.0, читаются только при миграции в `routing.json`.
- [backend/system/](backend/system/) — Windows-специфика: системный прокси через реестр с
  бэкапом, UAC-перезапуск, автозапуск, пикер процессов с иконками exe
  (`processes_windows.go`, WinAPI: Toolhelp32 + ExtractIconEx → PNG data-URL).

**Состояние и события.** Ядро толкает состояние через `Manager.OnState` → `App` эмитит в UI
`core:state`, `core:log`, `core:stats`, `core:mode`, `core:loglevel`, `profiles:changed`.
Секрет Clash API генерируется случайно на каждый запуск (`randomSecret`), порты фиксированы:
mixed 2080, Clash API 9090.

В payload `core:state` едет `since` — метка времени подключения (`App.connectedAt`, unix ms).
Ставится в ветке `StateRunning` через `CompareAndSwap(0, …)`, чтобы перезапуск ядра ради правил не
сбрасывал таймер, и обнуляется в `StateStopped`/`StateError`; для холодной загрузки есть
`App.GetStatus()`. **Фронт считает `now - since` на каждом тике**, а не инкрементирует счётчик:
таймеры в скрытом WebView2 троттлятся, и счётчик уплывёт за время в трее.

**Двуязычность (2.0.0+).** Язык хранится в `settings.Lang` (пусто = автоопределение по локали
Windows, `system.DefaultLang`), меняется на лету через `App.SetLanguage` — он же перетитровывает
меню трея (`applyTrayLang`, таблица `trayText` в [tray_windows.go](tray_windows.go); пункты в
`energye/systray` нельзя удалять, но можно переименовывать).

На фронте — свой стор `lib/i18n/` без зависимостей: `lang` (writable), `t` (derived) и `tp` для
числительных. Ключи плоские с точками, словари `ru.ts`/`en.ts` зеркальны, промах по ключу отдаёт
сам ключ — пропажа сразу видна в интерфейсе. **Всё, что зависит от языка, обязано читать `$t`/`$lang`
внутри реактивного выражения:** обычная функция с `tr()` посчитается один раз и застынет на языке
первого кадра (так уже ловились названия правил и единицы «Б/с»). По той же причине
`fmtBytes`/`fmtSpeed`/`fmtDate` в `store.ts` — derived-сторы, а не простые функции.

**Коды ошибок.** Wails отдаёт `error` в JS строкой, поэтому код везём префиксом:
`codedErr("E_TUN_RIGHTS", "…")` в [errcodes.go](errcodes.go) даёт `"[E_TUN_RIGHTS] текст"`, а
`errText()` на фронте вытаскивает префикс, ищет перевод `err.E_*` и при промахе показывает остаток
строки как есть — так вывод самого sing-box не теряется. Новую пользовательскую ошибку заводить
только через `codedErr`/`codedErrf` и сразу класть перевод в **оба** словаря.

**Маршрутизация (1.2.0+).** Никаких «режимов-пресетов»: есть один упорядоченный список правил,
первое совпавшее выигрывает. Изменение правила из UI применяется к живому ядру немедленно —
`App.applyRouting` генерирует конфиг, проверяет его `Manager.Check` (настоящий `sing-box check`) и
поднимает ядро через `Manager.Restart`. **Restart обязателен вместо `Stop`+`Start`:** пока взведён
флаг `restarting`, смерть процесса не порождает событие «остановлено», иначе `App.OnState` снял бы
системный прокси у активного соединения (см. app.go, ветка `StateStopped`). Флаг снимается **строго
после** возврата из `Stop`, а `Stop` ждёт закрытия канала `done` из `waitLoop` — то есть фактической
смерти процесса, а не смены состояния. Не «упрощать» до поллинга: сбросив флаг раньше, получим то
самое ложное «остановлено», а вернув `nil` при живом процессе — новое ядро на занятых портах 2080/9090.

Любая правка правил идёт через `App.withRouting`: снимок конфига → мутация → `applyRouting`, и при
ошибке ядра правила откатываются `rules.Replace(prev)`. Иначе на диске оседает правило, о котором
ядро не знает, — UI показывает одно, трафик идёт по-другому.

Поверх правил есть три режима Clash API (`Rule`/`Global`/`Direct`) — они переключаются на лету
через `PATCH /configs`, без перезапуска ядра. В конфиг попадают правила с `clash_mode`, включая
**холостое правило `{"clash_mode":"Rule","action":"sniff"}`**: ядро принимает только те режимы,
которые встретились в конфиге, и без этого правила из `Global` нельзя вернуться к `Rule`. Пустой
`route-options` в качестве заглушки не подходит — ядро его отвергает.

**Наблюдаемость (1.3.0+).** Вкладка «Соединения» (`ConnectionsTab.svelte`) раз в 1,5 с тянет
`App.GetConnections` → `GET /connections` Clash API: домен из sniff-а, цепочка outbound-ов,
сработавшее правило глазами ядра (`rule`/`rulePayload`), трафик и возраст. Оттуда же соединение
обрывается (`DELETE /connections/{id}`) и заводится правило по хосту/IP/процессу. Имя процесса ядро
заполняет **только** для соединений, попавших под правило по процессу, — у остальных колонка пуста,
это не баг.

Проверка домена (`App.CheckDomain` → `config.CheckDomain`) отвечает, куда уйдёт домен, до того как
по нему пойдёт трафик. Матчинг **не реализован у нас**: доменные условия всех включённых правил
собираются в один временный source-набор (`{"version":3,"rules":[…]}`), и его разбирает само ядро
командой `sing-box rule-set match` (`Manager.RuleSetMatch`) — вывод `match rules.[N]` даёт индексы
совпавших правил. Так семантика `domain_suffix`/`domain_regex` гарантированно та же, что в бою.
Правила по IP, порту и процессу помечаются пропущенными (`CheckSkip`): по одному домену судить о них
нельзя, и вживую они могут сработать раньше найденного.

**Удалённые наборы правил.** Описания живут в `routing.json` (`rules.RuleSet`), правило ссылается на
них по тегу тем же матчером `ruleSet`, что и на локальные `.srs`. Работают только потому, что
`experimental.cache_file` включён всегда. `download_detour` без нод откатывается на `direct` —
иначе конфиг ссылается на несуществующий outbound `proxy` и ядро не стартует.

**Текстовые списки доменов (`.lst`, 2.0.0+).** Формат `rules.FormatList` — наш, ядру не известный.
Список качает и конвертирует приложение ([backend/config/listset.go](backend/config/listset.go)),
результат кладётся в `data\rulesets\<tag>.json`, а в конфиг уезжает обычным **локальным**
`source`-набором. Две вещи, которые нельзя «упростить»:

- Точные имена и суффиксы — **разными правилами** в массиве `rules`: поля внутри одного правила
  ядро объединяет по «и», и `{"domain":[…],"domain_suffix":[…]}` требовало бы совпадения сразу по
  двум условиям. Голая строка `4pda.ru` даёт и `domain: 4pda.ru`, и `domain_suffix: .4pda.ru` —
  `domain_suffix` у sing-box это сравнение хвоста строки, поэтому суффикс без ведущей точки ловил
  бы ещё и `x4pda.ru`. Всё это проверено живым ядром в `TestListRuleSetValidates`.
- Скачивание — до записи в `routing.json` (`App.AddRuleSet`), иначе битый адрес превратился бы в
  набор, который ядро молча игнорирует. Дальше список докачивается тиком планировщика подписок
  (`refreshListSets`) и кнопкой `App.RefreshRuleSet`.

**Смена активного профиля** (`App.SetActiveProfile`) на живом соединении пересобирает конфиг и
перезапускает ядро через тот же `applyRouting`. Раньше метод только записывал ID, ядро продолжало
работать на прежних нодах, и «Сменить профиль» ничего не менял до переподключения.

Уровень журнала ядра — настройка (`settings.LogLevel`), она зашита в конфиг, поэтому применяется
перезапуском через тот же `applyRouting`; фильтр по уровню и подстроке в `LogsTab` работает поверх
уже накопленных строк и ядро не трогает.

**Безопасность выхода.** На любой остановке/аварии ядра `App` снимает системный прокси — иначе у
пользователя пропадёт интернет. Закрытие окна по умолчанию прячет в трей; реальный выход —
только через трей (`trayQuit`) либо когда трей не поднялся. От аварии самого GUI (краш, taskkill,
выключение питания) прокси в реестре остаётся — поэтому `startup` зовёт `SystemProxy.ClearStale`:
он снимает настройку, только если там стоит **наш** адрес (`sameProxyAddr`), чужие не трогает.
По той же причине `Set` не берёт в бэкап наш собственный адрес — иначе `Clear` вернул бы прокси
на мёртвый порт.

**Повреждённые файлы состояния.** `profile.Load`, `settings.Load` и `rules.Load` **всегда**
возвращают рабочее хранилище и ошибку рядом: nil-хранилище роняло бы половину методов `App`.
Битый файл переименовывается в `*.bad` (не перезаписывается молча), а `startup` показывает
уведомление.

**Потоки.** Меню трея живёт в цикле сообщений systray, состояние ядра приходит из горутины
`waitLoop`, вызовы UI — из горутин Wails. Поэтому глобалы трея закрыты `trayMu` (карта
`trayProfileIDs` без него давала фатальный `concurrent map read and map write`), а флаги
`App.trayQuit/relaunching/wasRunning/userStopping` — `atomic.Bool`. `go test -race` тут не
работает: нужен `CGO_ENABLED=1`.

**TUN и права.** TUN требует администратора: `Connect(true)` без прав вызывает
`system.RelaunchElevated(--tun-autostart)` и закрывает текущий процесс; новый процесс видит флаг и
сам поднимает соединение. Из-за этого single-instance lock в [main.go](main.go) отключается при
наличии `--tun-autostart`.

Передача управления хрупкая, тут два неочевидных условия — не «упрощать»:

- Перед `runtime.Quit` обязателен флаг `App.relaunching`, иначе `beforeClose` спрячет окно в трей
  и старый процесс останется жить (симптом — вторая иконка в трее и **пустое окно** у нового).
- Обычный и elevated процессы делят WebView2 user data folder (`%APPDATA%\MitM.exe\EBWebView`),
  но одновременно держать её не могут — у второго движок не поднимется и окно останется залитым
  одним `BackgroundColour`. `ShellExecuteW` возвращает управление сразу после старта нового
  процесса, поэтому предок в этот момент ещё жив: ему передаётся `--wait-pid=<pid>`, и новый
  процесс перед `wails.Run` ждёт (`system.WaitForProcessExit` + `WaitForWebviewRelease` по
  эксклюзивному `lockfile`). Оба ожидания с потолком — подвисший предок не должен блокировать старт.

**Портативность путей.** `core.ResolvePaths` ищет ассеты рядом с exe, затем в cwd (для `wails dev`);
данные пишет в `data\` рядом с exe, с фолбэком на `%LOCALAPPDATA%\MitM`. Сохранённый путь к
альтернативному ядру резолвится через `App.resolveCorePath`: если абсолютный путь не найден
(другая буква диска), ищется файл с тем же именем в `assets` — поэтому имя форка обязано быть
уникальным (`sing-box-xhttp.exe`, не `sing-box.exe`).

## Грабли sing-box (проверено на живых нодах)

Всё это уже зашито в `generator.go` — не «упрощать» обратно.

- **`geoip.db`/`geosite.db` не работают в route-правилах** начиная с sing-box 1.12: только
  `rule_set` с локальными `.srs`. Готовые лежат в `assets\` (`geoip-ru`, `geosite-ru`,
  `geosite-ads`); генерируются офлайн: `sing-box geoip export ru` → JSON → `sing-box rule-set
  compile`. Приватные сети — через `ip_is_private: true`, категория РФ — `category-ru`.
- **DNS в TUN:** нужен DoH-remote через прокси + `hijack-dns` + `default_domain_resolver`, иначе
  UDP DNS не проходит через VLESS+Vision. Локальному dns-серверу **не** ставить `detour: "direct"`
  — фатальная ошибка в 1.12+.
- **QUIC в TUN:** на TCP-нодах (vless-vision, xhttp) UDP:443 уходит в чёрную дыру и браузер виснет
  на HTTP/3 — симптом «Google не грузит картинки, текст идёт». Фикс: route-правило
  `{"protocol":"quic","action":"reject"}` только при TUN (настройка `AllowQUIC`).
- **IPv6 в TUN:** отдельный баг с похожим симптомом. DNS-стратегия `ipv4_only` и TUN Address только
  IPv4 (`172.19.0.1/30`) — иначе на нодах без IPv6-выхода dual-stack сайты уходят в никуда.
- **`tls_fragment` осмысленен только с `direct`.** Замерено на живых ядрах: ядро режет ту полезную
  нагрузку, которую пишет **в outbound**, а не сам сокет до сервера. У `direct` это и есть провод —
  ClientHello уезжает двумя TCP-записями (285 Б → 117 + 168), SNI оказывается на границе, простой
  DPI его не собирает. У прокси-правила разрез ложится **внутрь туннеля** и наружу уходит внутри
  шифрования: провайдер видит ровно то же, что и без флага, платится только задержка. Поэтому
  `route.go` ставит поле лишь при `ActionDirect`, `Rule.Validate` отвергает остальные действия, а
  `Config.Normalize` гасит флаг в старых `routing.json` (до 2.0.0 UI разрешал его и на прокси) —
  иначе строгая валидация уронила бы первое же сохранение правил.
- **Пауза между кусками задаётся явно.** Собственный дефолт ядра — `tls_fragment_fallback_delay`
  500 мс, и штатное 1.13 берёт его **всегда**: рукопожатие с example.com росло со 135 мс до 630 мс,
  и так на каждом соединении под правилом (форк 1.14 умеет выводить паузу из RTT и добавлял ~65 мс).
  При этом разрез на две TCP-записи происходит при любой ненулевой паузе — замерено от 1 мс, —
  поэтому в конфиг зашиты `80ms` (`tlsFragmentFallbackDelay` в `route.go`). Ноль ставить нельзя:
  ядро считает его «не задано» и возвращается к своим 500 мс.

## Два ядра / XHTTP

Штатный sing-box не поддерживает транспорт `xhttp`/`splithttp`. Для таких нод в комплекте форк
[Leadaxe/sing-box-lx](https://github.com/Leadaxe/sing-box-lx) (build-тег `with_xhttp`) как
`assets\sing-box-xhttp.exe`; выбор ядра — в UI, путь пишется в `settings.json` (`corePath`),
переключение — `Manager.SetBinaryPath`. `wintun.dll` копируется в `DataDir`
(`Paths.EnsureWintun`), чтобы TUN работал с ядром вне `assets`. Разбор xhttp-ссылок — `buildXHTTP`
в `parser.go` (слияние параметра `extra` c camelCase→snake_case).

## Лицензии

GUI авторский; sing-box и его форк — GPLv3, подключаются **только как отдельные процессы**. Не
линковать их код в бинарь.
