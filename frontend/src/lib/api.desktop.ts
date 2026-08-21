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
