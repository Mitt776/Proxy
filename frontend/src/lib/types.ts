// Типы, общие для нескольких компонентов.

/**
 * AppInfo — сводка окружения из Go (см. AppInfo в app.go). Держим тип в одном
 * месте: раньше каждый компонент описывал форму объекта у себя, и добавление
 * поля в Go ломало проверку типов в случайных местах.
 */
export interface AppInfo {
  appVersion: string;
  coreVersion: string;
  coreFound: boolean;
  corePath: string;
  coreCustom: boolean;
  assetsDir: string;
  dataDir: string;
  state: string;
  since: number;
  isAdmin: boolean;
  lang: string;
  startHidden: boolean;
}
