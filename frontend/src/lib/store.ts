// Общее состояние UI. Вынесено из App.svelte, чтобы вкладки не передавали
// друг другу пропсы через всю иерархию: состояние ядра и всплывающие
// сообщения нужны почти каждому компоненту.
import { writable, derived } from "svelte/store";

/** Состояние ядра: stopped | starting | running | error. */
export const coreState = writable("stopped");

/** Подключено ли сейчас — самая частая проверка в UI. */
export const connected = derived(coreState, ($s) => $s === "running");

/** Последняя ошибка, показываемая пользователю (пусто — ошибок нет). */
export const errorText = writable("");

/** Текст всплывающего уведомления (пусто — скрыто). */
export const toastText = writable("");

let toastTimer: ReturnType<typeof setTimeout>;

/** showToast показывает короткое подтверждение действия. */
export function showToast(msg: string) {
  toastText.set(msg);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastText.set(""), 1800);
}

/** reportError кладёт ошибку в общий баннер, приводя её к строке. */
export function reportError(e: unknown) {
  errorText.set(String(e));
}

/** fmtBytes форматирует объём в человекочитаемый вид. */
export function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0,
    v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

/** fmtSpeed — то же, но в секунду. */
export const fmtSpeed = (n: number) => fmtBytes(n) + "/s";

/** fmtDate приводит дату из Go (RFC3339) к локальному виду. */
export function fmtDate(v: any): string {
  if (!v) return "";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "" : d.toLocaleString();
}
