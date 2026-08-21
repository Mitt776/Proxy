// Общее состояние UI. Вынесено из App.svelte, чтобы вкладки не передавали
// друг другу пропсы через всю иерархию: состояние ядра и всплывающие
// сообщения нужны почти каждому компоненту.
import { derived, writable } from "svelte/store";
import { EventsOn } from "$api";
import { GetStatus } from "$api";
import { errText } from "./i18n/errors";
import { lang } from "./i18n";

/** Состояние ядра: stopped | starting | running | error. */
export const coreState = writable("stopped");

/** Подключено ли сейчас — самая частая проверка в UI. */
export const connected = derived(coreState, ($s) => $s === "running");

/**
 * Момент начала сессии в unix-миллисекундах (0 — не подключены). Приходит из Go
 * и служит опорной точкой для таймера: экран считает `now - since` на каждом
 * тике, а не крутит собственный счётчик. В свёрнутом окне WebView2 троттлит
 * таймеры, и счётчик отстал бы на минуты.
 */
export const since = writable(0);

/** Последняя ошибка, показываемая пользователю (пусто — ошибок нет). */
export const errorText = writable("");

/** Текст всплывающего уведомления (пусто — скрыто). */
export const toastText = writable("");

// --- Трафик ---

export const downSpeed = writable(0);
export const upSpeed = writable(0);
export const downTotal = writable(0);
export const upTotal = writable(0);
export const connCount = writable(0);

// HIST_LEN — сколько точек держим для спарклайнов (при тике раз в секунду — минута).
const HIST_LEN = 60;

/**
 * История для графиков. Живёт в сторе, а не в компонентах, сознательно: вкладки
 * монтируются через {#if}, и локальный буфер обнулялся бы при каждом
 * переключении — график начинался бы с нуля после каждого перехода.
 */
export const downHist = writable<number[]>([]);
export const upHist = writable<number[]>([]);
export const connHist = writable<number[]>([]);
export const latHist = writable<number[]>([]);

/** pushHist добавляет точку, удерживая длину буфера. */
export function pushHist(store: typeof downHist, v: number) {
  store.update((a) => {
    const next = a.length >= HIST_LEN ? a.slice(a.length - HIST_LEN + 1) : a.slice();
    next.push(v);
    return next;
  });
}

function resetTraffic() {
  downSpeed.set(0);
  upSpeed.set(0);
  downTotal.set(0);
  upTotal.set(0);
  connCount.set(0);
  downHist.set([]);
  upHist.set([]);
  connHist.set([]);
  latHist.set([]);
}

/**
 * initStore подписывается на события ядра один раз за жизнь окна. Раньше каждый
 * компонент подписывался сам, и после переключения вкладки история графиков и
 * состояние приходилось собирать заново.
 */
export async function initStore() {
  try {
    const st = await GetStatus();
    applyState(st.state, st.since);
  } catch (e) {
    console.error("GetStatus:", e);
  }

  EventsOn("core:state", (p: { state: string; reason?: string; since?: number }) => {
    applyState(p.state, p.since ?? 0);
    if (p.reason) errorText.set(errText(p.reason));
  });

  EventsOn(
    "core:stats",
    (p: {
      downSpeed: number;
      upSpeed: number;
      downTotal: number;
      upTotal: number;
      connections: number;
    }) => {
      downSpeed.set(p.downSpeed);
      upSpeed.set(p.upSpeed);
      downTotal.set(p.downTotal);
      upTotal.set(p.upTotal);
      connCount.set(p.connections);
      pushHist(downHist, p.downSpeed);
      pushHist(upHist, p.upSpeed);
      pushHist(connHist, p.connections);
    },
  );
}

function applyState(state: string, sinceMs: number) {
  coreState.set(state);
  since.set(sinceMs || 0);
  // Цвет состояния держим на <html>: от него красятся кнопка, точки и рамки,
  // и ни один компонент не обязан знать про остальные.
  document.documentElement.dataset.state = state;
  if (state !== "running" && state !== "starting") resetTraffic();
}

let toastTimer: ReturnType<typeof setTimeout>;

/** showToast показывает короткое подтверждение действия. */
export function showToast(msg: string) {
  toastText.set(msg);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastText.set(""), 1800);
}

/** reportError кладёт ошибку в общий баннер, переводя её по коду. */
export function reportError(e: unknown) {
  errorText.set(errText(e));
}

// --- Форматирование ---

const UNITS: Record<string, string[]> = {
  ru: ["Б", "КБ", "МБ", "ГБ", "ТБ"],
  en: ["B", "KB", "MB", "GB", "TB"],
};

function bytes(l: string, n: number): string {
  const u = UNITS[l] ?? UNITS.en;
  if (!n) return `0 ${u[0]}`;
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}

// Форматтеры — сторы, а не простые функции: у них есть единицы измерения, а
// значит и язык. Обычная функция при переключении RU/EN не заставила бы Svelte
// пересчитать разметку, и рядом с английскими подписями оставались бы «Б/с».
// В разметке используются как $fmtBytes(n) — так же, как $t("ключ").

/** fmtBytes форматирует объём в человекочитаемый вид на текущем языке. */
export const fmtBytes = derived(lang, ($l) => (n: number) => bytes($l, n));

/** fmtSpeed — то же, но в секунду. */
export const fmtSpeed = derived(
  lang,
  ($l) =>
    (n: number): string =>
      bytes($l, n) + ($l === "ru" ? "/с" : "/s"),
);

/** fmtDate приводит дату из Go (RFC3339) к локальному виду. */
export const fmtDate = derived(
  lang,
  ($l) =>
    (v: any): string => {
      if (!v) return "";
      const d = new Date(v);
      return isNaN(d.getTime()) ? "" : d.toLocaleString($l);
    },
);

/** fmtDuration переводит миллисекунды в ЧЧ:ММ:СС для таймера сессии. */
export function fmtDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(h)}:${p(m)}:${p(s)}`;
}
