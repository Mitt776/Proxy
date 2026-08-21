// Шов между интерфейсом и Go, десктопная сторона.
//
// Общие компоненты в lib/ импортируют всё из "$api"; какой файл за этим алиасом —
// решает vite по точке входа сборки (см. vite.config.ts). Здесь просто реэкспорт
// того, что сгенерировал Wails.
//
// Каталог wailsjs/ генерируется `wails build` и в git не хранится.

export * from "../../wailsjs/go/main/App";

export {
  EventsOn,
  ClipboardSetText,
  ClipboardGetText,
  Quit,
  WindowMinimise,
} from "../../wailsjs/runtime/runtime";

import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { ImportQRImage } from "../../wailsjs/go/main/App";

// --- то, чего у Wails нет ---
//
// Эти имена существуют ради общей поверхности `$api`: их импортируют мобильные
// экраны, а svelte-check проверяет весь проект против десктопной реализации.
// Поэтому здесь либо честный десктопный аналог, либо ошибка с кодом — молча
// возвращать undefined нельзя, иначе промах увидят не мы, а пользователь.

/** Открыть ссылку во внешнем браузере. */
export function OpenURL(url: string): Promise<void> {
  BrowserOpenURL(url);
  return Promise.resolve();
}

/**
 * Выбрать картинку с QR и завести по ней профиль. На Windows это ровно тот же
 * файловый диалог, что и ImportQRImage, — отличается только тип результата:
 * мобильной стороне достаточно имени профиля.
 */
export async function PickQRImage(): Promise<string> {
  const p = await ImportQRImage();
  return p?.name ?? "";
}

/** Сканирование QR камерой — только на телефоне. */
export function ScanQR(): Promise<string> {
  return Promise.reject(new Error("[E_NO_METHOD] сканирование камерой доступно только на Android"));
}

/** Список установленных приложений — только на телефоне. */
export function ListApps(): Promise<
  { package: string; label: string; icon: string; system: boolean }[]
> {
  return Promise.reject(new Error("[E_NO_METHOD] выбор приложений доступен только на Android"));
}

export function GetExcludedApps(): Promise<string[]> {
  return Promise.resolve([]);
}

export function SetExcludedApps(_packages: string[]): Promise<void> {
  return Promise.reject(new Error("[E_NO_METHOD] выбор приложений доступен только на Android"));
}

export function CheckUpdate(): Promise<{ available: boolean; version: string; url: string }> {
  return Promise.reject(new Error("[E_NO_METHOD] проверка обновлений пока только на Android"));
}

export function SetUpdateCheck(_on: boolean): Promise<void> {
  return Promise.reject(new Error("[E_NO_METHOD] проверка обновлений пока только на Android"));
}
