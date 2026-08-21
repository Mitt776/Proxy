// Шов между интерфейсом и Go, мобильная сторона.
//
// Здесь нет Wails: WebView разговаривает с Go через мост, который вешает Kotlin
// (см. android/.../Bridge.kt). Поверхность повторяет сгенерированные Wails
// биндинги — все методы возвращают промис, — поэтому общие компоненты в lib/ не
// знают, на какой платформе работают.

/** MitMNative — мост, который Kotlin добавляет в WebView через addJavascriptInterface. */
interface MitMNative {
  call(id: number, method: string, argsJSON: string): void;
  clipboardGet(): string;
  clipboardSet(text: string): void;
}

declare global {
  interface Window {
    MitMNative?: MitMNative;
    /** Ответ на call — зовётся Kotlin из evaluateJavascript. */
    __mitmResolve?: (id: number, resultJSON: string, errText: string) => void;
    /** Событие из Go — тот же поток, что EventsOn на Windows. */
    __mitmEvent?: (name: string, payloadJSON: string) => void;
    /**
     * Аппаратная кнопка «назад». Вешается оболочкой (AppMobile.svelte), зовётся
     * из MainActivity; true означает «нажатие обработали», false — сворачивай
     * приложение. Синхронно: evaluateJavascript отдаёт в колбэк возвращённое
     * значение, дождаться промиса он не умеет.
     */
    __mitmBack?: () => boolean;
  }
}

type Pending = { resolve: (value: any) => void; reject: (reason: any) => void };

const pending = new Map<number, Pending>();
let nextID = 1;

window.__mitmResolve = (id, resultJSON, errText) => {
  const entry = pending.get(id);
  if (!entry) return;
  pending.delete(id);
  if (errText) {
    // Ошибку отдаём строкой, как это делает Wails: код едет префиксом
    // "[E_CODE] текст", и errText() на фронте разбирает её без изменений.
    entry.reject(new Error(errText));
    return;
  }
  try {
    entry.resolve(resultJSON ? JSON.parse(resultJSON) : null);
  } catch (e) {
    entry.reject(e);
  }
};

/**
 * call — вызов метода Go.
 *
 * Мост намеренно асинхронный: синхронный @JavascriptInterface заблокировал бы
 * поток WebView на обновлении подписки или тесте задержки, а это секунды с
 * замершим интерфейсом.
 */
function call<T = any>(method: string, ...args: any[]): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const native = window.MitMNative;
    if (!native) {
      reject(new Error("[E_NOT_READY] мост не подключён"));
      return;
    }
    const id = nextID++;
    pending.set(id, { resolve, reject });
    try {
      native.call(id, method, JSON.stringify(args));
    } catch (e) {
      pending.delete(id);
      reject(e);
    }
  });
}

// --- события ---

type Handler = (...data: any[]) => void;

const listeners = new Map<string, Set<Handler>>();

window.__mitmEvent = (name, payloadJSON) => {
  const set = listeners.get(name);
  if (!set || set.size === 0) return;
  let payload: any = null;
  try {
    payload = payloadJSON ? JSON.parse(payloadJSON) : null;
  } catch {
    payload = payloadJSON;
  }
  for (const handler of set) handler(payload);
};

/** EventsOn повторяет семантику Wails: возвращает функцию отписки. */
export function EventsOn(name: string, handler: Handler): () => void {
  let set = listeners.get(name);
  if (!set) {
    set = new Set();
    listeners.set(name, set);
  }
  set.add(handler);
  return () => {
    set!.delete(handler);
  };
}

// --- буфер обмена и окно ---

export function ClipboardSetText(text: string): Promise<boolean> {
  window.MitMNative?.clipboardSet(text);
  return Promise.resolve(true);
}

export function ClipboardGetText(): Promise<string> {
  return Promise.resolve(window.MitMNative?.clipboardGet() ?? "");
}

// Кнопок окна на телефоне нет — заглушки, чтобы общая шапка собиралась.
export function Quit(): Promise<void> {
  return Promise.resolve();
}

export function WindowMinimise(): Promise<void> {
  return Promise.resolve();
}

// --- методы приложения ---
// Порядок и имена — как в сгенерированных Wails биндингах. Методы, которых на
// Android нет (ListProcesses, CheckDomain, соединения, автозапуск, выбор ядра),
// здесь отсутствуют: их и в мобильных экранах нет, а промах по имени должен
// падать заметно, а не молча возвращать undefined.

export const GetAppInfo = () => call("GetAppInfo");
export const GetState = () => call<string>("GetState");
export const GetStatus = () => call("GetStatus");
export const GetLogs = () => call<string[]>("GetLogs");
export const GetLanguage = () => call<string>("GetLanguage");
export const SetLanguage = (lang: string) => call<void>("SetLanguage", lang);

export const Connect = (enableTUN: boolean) => call<void>("Connect", enableTUN);
export const Disconnect = () => call<void>("Disconnect");

export const ListProfiles = () => call<any[]>("ListProfiles");
export const GetActiveProfileID = () => call<string>("GetActiveProfileID");
export const AddManualProfile = (name: string, raw: string) => call("AddManualProfile", name, raw);
export const AddSubscriptionProfile = (name: string, url: string) =>
  call("AddSubscriptionProfile", name, url);
export const RefreshProfile = (id: string) => call("RefreshProfile", id);
export const DeleteProfile = (id: string) => call<void>("DeleteProfile", id);
export const SetActiveProfile = (id: string) => call<void>("SetActiveProfile", id);
export const ListProfileNodes = (id: string) => call<any[]>("ListProfileNodes", id);
export const ProfileConfigJSON = (id: string) => call<string>("ProfileConfigJSON", id);
export const ProfileRaw = (id: string) => call<string>("ProfileRaw", id);
export const ProfileQR = (id: string) => call<string>("ProfileQR", id);

export const GetRouting = () => call("GetRouting");
export const SetRouting = (cfg: any) => call<void>("SetRouting", cfg);
export const AddRule = (rule: any) => call<string>("AddRule", rule);
export const UpdateRule = (rule: any) => call<void>("UpdateRule", rule);
export const DeleteRule = (id: string) => call<void>("DeleteRule", id);
export const MoveRule = (id: string, index: number) => call<void>("MoveRule", id, index);
export const SetRuleEnabled = (id: string, enabled: boolean) =>
  call<void>("SetRuleEnabled", id, enabled);
export const SetRoutingFinal = (final: string) => call<void>("SetRoutingFinal", final);
export const AddGroup = (group: any) => call<string>("AddGroup", group);
export const UpdateGroup = (group: any) => call<void>("UpdateGroup", group);
export const DeleteGroup = (id: string) => call<void>("DeleteGroup", id);

export const ListRuleSets = () => call<any[]>("ListRuleSets");
export const AddRuleSet = (rs: any) => call<string>("AddRuleSet", rs);
export const UpdateRuleSet = (rs: any) => call<void>("UpdateRuleSet", rs);
export const DeleteRuleSet = (id: string) => call<void>("DeleteRuleSet", id);
export const RefreshRuleSet = (id: string) => call<number>("RefreshRuleSet", id);

export const GetMode = () => call<string>("GetMode");
export const SetMode = (mode: string) => call<void>("SetMode", mode);
export const GetProxies = () => call("GetProxies");
export const SelectNode = (name: string) => call<void>("SelectNode", name);
export const TestDelay = (name: string) => call<number>("TestDelay", name);
export const ExternalIP = () => call("ExternalIP");

export const GetSettings = () => call("GetSettings");
export const SetBlockQUIC = (block: boolean) => call<void>("SetBlockQUIC", block);
export const SetSubUpdateHours = (hours: number) => call<void>("SetSubUpdateHours", hours);
export const GetLogLevel = () => call<string>("GetLogLevel");
export const SetLogLevel = (level: string) => call<void>("SetLogLevel", level);

export const GetExcludedApps = () => call<string[]>("GetExcludedApps");
export const SetExcludedApps = (packages: string[]) => call<void>("SetExcludedApps", packages);

export const CheckUpdate = () =>
  call<{ available: boolean; version: string; url: string; notes: string }>("CheckUpdate");
export const SetUpdateCheck = (on: boolean) => call<void>("SetUpdateCheck", on);

// --- то, что делает сама Kotlin ---
//
// Эти методы не доезжают до Go-диспетчера: их перехватывает Bridge, потому что
// каждый открывает системный экран (пикер картинки, камеру, список приложений)
// и отвечает только после того, как пользователь оттуда вернулся. Протокол тот
// же самый — ответ приходит в __mitmResolve по тому же id.

/** Выбрать картинку из галереи и завести профиль по QR. «» — отменено. */
export const PickQRImage = () => call<string>("PickQRImage");

/** Навести камеру на QR и завести профиль. «» — отменено. */
export const ScanQR = () => call<string>("ScanQR");

/**
 * Установленные приложения с иконками — для выбора тех, что пойдут мимо VPN.
 * Список рисуем сами в WebView, как на Windows это делает выбор процесса: так у
 * экрана те же токены оформления и та же двуязычность, а не вторая вёрстка на
 * Kotlin, которую пришлось бы переводить отдельно.
 */
export const ListApps = () =>
  call<{ package: string; label: string; icon: string; system: boolean }[]>("ListApps");

/** Открыть ссылку в браузере телефона. */
export const OpenURL = (url: string) => call<void>("OpenURL", url);

// ImportQRImage (файловый диалог Windows) здесь отсутствует намеренно: картинку
// приносит системный пикер, см. PickQRImage.
