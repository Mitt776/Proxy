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
