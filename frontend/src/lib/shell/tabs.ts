// Список разделов в порядке показа. Вынесен из компонентов, потому что нужен и
// сайдбару (пункты меню), и App.svelte (что рисовать в области контента).
export const TABS = [
  "connect",
  "profiles",
  "routing",
  "traffic",
  "logs",
  "settings",
] as const;

export type Tab = (typeof TABS)[number];

/**
 * Разделы мобильной сборки. «Трафик» выпал сознательно: вкладка построена вокруг
 * широкой таблицы соединений, а `GetConnections` в мобильном мосте нет вовсе —
 * см. заголовок mobile/dispatch_android.go.
 */
export const MOBILE_TABS = [
  "connect",
  "profiles",
  "routing",
  "logs",
  "settings",
] as const satisfies readonly Tab[];

export type MobileTab = (typeof MOBILE_TABS)[number];
