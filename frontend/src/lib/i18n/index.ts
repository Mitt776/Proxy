// Двуязычность интерфейса. Свой минимальный стор вместо библиотеки: в проекте
// ноль runtime-зависимостей, а нужно нам ровно два действия — достать строку и
// сменить язык. Смена работает на лету: `lang` — обычный writable, Svelte сам
// перерисовывает всё, что читает `$t`, перезагрузка окна не нужна.
import { derived, get, writable } from "svelte/store";
import { SetLanguage } from "$api";
import { ru } from "./ru";
import { en } from "./en";

export type Lang = "ru" | "en";

/** Словари. Ключи плоские, с точками: "connect.status.running". */
const dicts: Record<Lang, Record<string, string>> = { ru, en };

/** Текущий язык. Начальное значение приходит из Go (settings + локаль Windows). */
export const lang = writable<Lang>("ru");

/** Подстановки вида {name} в строке. */
export type Vars = Record<string, string | number>;

function translate(l: Lang, key: string, vars?: Vars): string {
  // Промах по ключу отдаём самим ключом, а не пустотой: в интерфейсе сразу
  // видно, чего не хватает, вместо молча пропавшей подписи.
  const raw = dicts[l][key] ?? dicts.ru[key] ?? key;
  if (!vars) return raw;
  return raw.replace(/\{(\w+)\}/g, (m, name) =>
    name in vars ? String(vars[name]) : m,
  );
}

/**
 * Переводчик. Используется как `$t("connect.title")` — зависимость от `lang`
 * заставляет Svelte пересчитать разметку при смене языка.
 */
export const t = derived(
  lang,
  ($l) =>
    (key: string, vars?: Vars): string =>
      translate($l, key, vars),
);

/** tr — разовый перевод вне разметки (обработчики, вычисления). */
export function tr(key: string, vars?: Vars): string {
  return translate(get(lang), key, vars);
}

/**
 * plural выбирает форму числительного. В русском их три («1 нода», «2 ноды»,
 * «5 нод»), в английском две — поэтому en всегда падает в `many`, а ключ `few`
 * ему просто не нужен.
 */
function plural(l: Lang, n: number): "one" | "few" | "many" {
  if (l !== "ru") return n === 1 ? "one" : "many";
  const d = n % 10;
  const h = n % 100;
  if (d === 1 && h !== 11) return "one";
  if (d >= 2 && d <= 4 && (h < 12 || h > 14)) return "few";
  return "many";
}

/**
 * tp — перевод с числом: ищет ключ с суффиксом формы («profiles.nodes.one»)
 * и подставляет {n}. Используется как `$tp("profiles.nodes", count)`.
 */
export const tp = derived(
  lang,
  ($l) =>
    (key: string, n: number, vars?: Vars): string =>
      translate($l, key + "." + plural($l, n), { n, ...vars }),
);

/** initLang выставляет язык, полученный от Go, не дёргая настройки обратно. */
export function initLang(l: string) {
  lang.set(l === "en" ? "en" : "ru");
  applyDocumentLang();
}

/**
 * setLang меняет язык интерфейса и сохраняет выбор. В Go он нужен не только
 * ради settings.json: меню трея живёт вне фронтенда и переводится там же.
 */
export async function setLang(l: Lang) {
  lang.set(l);
  applyDocumentLang();
  try {
    await SetLanguage(l);
  } catch (e) {
    // Не сохранилось — интерфейс всё равно уже переключён; на следующем запуске
    // вернётся прежний язык, но ронять из-за этого нечего.
    console.error("SetLanguage:", e);
  }
}

/** Атрибут lang на <html> — от него зависят переносы и выбор шрифта. */
function applyDocumentLang() {
  document.documentElement.lang = get(lang);
}
