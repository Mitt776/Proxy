# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## О проекте

Портативный прокси-клиент для Windows: GUI на Wails v2 (Go + WebView2 + Svelte/TS), сетевое
ядро — **внешний процесс** `sing-box.exe`, которым управляем через сгенерированный `config.json`
и Clash API. Ядро в репозитории не хранится (см. `.gitignore`). Комментарии и UI — на русском.

## Команды

```powershell
wails build                 # релизная сборка → build\bin\Proxy.exe
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
job-object (job_windows_test.go).

### Портативный релиз

`wails build` не копирует ассеты — после сборки вручную положить `assets\*.exe`, `*.dll`, `*.srs`
в `build\bin\assets\` и вычистить `build\bin\data\`.

## Архитектура

**Слои.** Фронтенд (`frontend/src/App.svelte`, один файл) → биндинги Wails
(`frontend/wailsjs/`, генерируются, в git не хранятся) → `App` в [app.go](app.go) — единственный
объект, забинденный в JS; все публичные методы `App` = API для UI. Ниже — `backend/*` пакеты,
не знающие ни о Wails, ни друг о друге, кроме `profile → config`.

**Поток подключения:** `App.Connect(enableTUN)` → `store.ResolveNodes` → `config.Generate(Options)`
→ `core.Manager.Start(json)` → запись `data\config.json` → `sing-box.exe run -c ... -D data`
→ при `!enableTUN` ещё и `system.SystemProxy.Set(127.0.0.1:2080)`.

**Пакеты:**

- [backend/config/](backend/config/) — разбор входа и генерация конфига ядра.
  `parser.go`/`parser_protocols.go` — ссылки `vless://` и др. в outbound-JSON; `clash.go` —
  Clash YAML; `subscription.go` — автоопределение формата (sing-box JSON / Clash / base64 / список
  ссылок); `generator.go` — сборка полного sing-box-конфига (DNS, route, inbounds, outbounds).
  Ключевые теги outbound-ов: `proxy` (selector), `auto` (urltest), `direct`.
- [backend/core/](backend/core/) — жизненный цикл ядра. `manager.go` — старт/стоп, кольцевой буфер
  логов, колбэки `OnLog`/`OnState`; `paths.go` — резолв assets/data (портативность);
  `clashapi.go` — HTTP-клиент Clash API (список нод, выбор, задержки, трафик);
  `proc_windows.go` — job object, убивающий ядро вместе с GUI.
- [backend/profile/](backend/profile/) — профили (ручные и подписки) в `data\profiles.json`.
- [backend/settings/](backend/settings/) — `data\settings.json`; при добавлении поля помни, что
  zero-value применяется к старым файлам (см. инверсию `AllowQUIC`).
- [backend/system/](backend/system/) — Windows-специфика: системный прокси через реестр с
  бэкапом, UAC-перезапуск, автозапуск.

**Состояние и события.** Ядро толкает состояние через `Manager.OnState` → `App` эмитит в UI
`core:state`, `core:log`, `core:stats`, `profiles:changed`. Секрет Clash API генерируется случайно
на каждый запуск (`randomSecret`), порты фиксированы: mixed 2080, Clash API 9090.

**Безопасность выхода.** На любой остановке/аварии ядра `App` снимает системный прокси — иначе у
пользователя пропадёт интернет. Закрытие окна по умолчанию прячет в трей; реальный выход —
только через трей (`trayQuit`) либо когда трей не поднялся.

**TUN и права.** TUN требует администратора: `Connect(true)` без прав вызывает
`system.RelaunchElevated(--tun-autostart)` и закрывает текущий процесс; новый процесс видит флаг и
сам поднимает соединение. Из-за этого single-instance lock в [main.go](main.go) отключается при
наличии `--tun-autostart`.

Передача управления хрупкая, тут два неочевидных условия — не «упрощать»:

- Перед `runtime.Quit` обязателен флаг `App.relaunching`, иначе `beforeClose` спрячет окно в трей
  и старый процесс останется жить (симптом — вторая иконка в трее и **пустое окно** у нового).
- Обычный и elevated процессы делят WebView2 user data folder (`%APPDATA%\Proxy.exe\EBWebView`),
  но одновременно держать её не могут — у второго движок не поднимется и окно останется залитым
  одним `BackgroundColour`. `ShellExecuteW` возвращает управление сразу после старта нового
  процесса, поэтому предок в этот момент ещё жив: ему передаётся `--wait-pid=<pid>`, и новый
  процесс перед `wails.Run` ждёт (`system.WaitForProcessExit` + `WaitForWebviewRelease` по
  эксклюзивному `lockfile`). Оба ожидания с потолком — подвисший предок не должен блокировать старт.

**Портативность путей.** `core.ResolvePaths` ищет ассеты рядом с exe, затем в cwd (для `wails dev`);
данные пишет в `data\` рядом с exe, с фолбэком на `%LOCALAPPDATA%\Proxy`. Сохранённый путь к
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
