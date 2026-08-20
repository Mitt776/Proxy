// Перевод ошибок, пришедших из Go.
//
// Wails отдаёт `error` в JS обычной строкой, поэтому код едет префиксом в самом
// тексте: "[E_NO_PROFILE] не выбран активный профиль" (см. errcodes.go). Здесь
// префикс вырезается и меняется на перевод.
//
// Фолбэк принципиален: под кодами ходят только наши собственные ошибки, а вывод
// самого sing-box, ошибки файловой системы и сети приходят без префикса — их
// показываем как есть, иначе пользователь потеряет единственную подсказку о том,
// что на самом деле пошло не так.
import { tr } from "./index";

const CODE_RE = /^\[([A-Z0-9_]+)\]\s*/;

/** errText приводит любую ошибку к строке на текущем языке интерфейса. */
export function errText(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e ?? "");
  const m = raw.match(CODE_RE);
  if (!m) return raw;

  const code = m[1];
  const rest = raw.slice(m[0].length);
  const key = "err." + code;
  const translated = tr(key);
  if (translated === key) return rest; // код неизвестен — остаётся русский текст

  // Обёртки (%w) дописывают к нашему сообщению подробности от ядра — они несут
  // самую полезную часть, поэтому переведённый заголовок дополняем ими.
  const detail = detailOf(rest, code);
  return detail ? `${translated}: ${detail}` : translated;
}

/**
 * detailOf вытаскивает «хвост» после двоеточия — то, что дописала обёртка %w.
 * Русский текст самого сообщения при этом отбрасывается: его уже заменил перевод.
 */
function detailOf(rest: string, code: string): string {
  if (code === "E_CORE_CHECK") return rest; // здесь весь текст — вывод ядра
  const i = rest.indexOf(": ");
  return i === -1 ? "" : rest.slice(i + 2);
}
