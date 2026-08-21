import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { fileURLToPath } from 'node:url'

// Две сборки из одного проекта: десктопная (Wails) и мобильная (WebView в APK).
// Общий код в src/lib/ обращается к Go только через алиас $api — здесь решается,
// какая из двух реализаций за ним стоит.
//
// Мобильную сборку включает `vite build --mode android` (npm run build:mobile).
// Режим vite, а не переменная окружения: `set VAR=…` работает только в cmd.exe,
// и та же команда на другой машине молча собрала бы десктопный вариант.

const resolvePath = (relative: string) =>
  fileURLToPath(new URL(relative, import.meta.url))

export default defineConfig(({ mode }) => {
  const isAndroid = mode === 'android'
  return {
    plugins: [svelte()],
    resolve: {
      alias: {
        $api: resolvePath(isAndroid ? './src/lib/api.android.ts' : './src/lib/api.desktop.ts'),
      },
    },
    // Пути относительно страницы: интерфейс отдаётся из ассетов APK, и абсолютный
    // /assets/... уехал бы в корень домена мимо каталога web/.
    base: isAndroid ? './' : '/',
    build: isAndroid
      ? {
          // Сборка кладётся прямо в ассеты APK — Gradle упакует каталог как есть.
          outDir: '../android/app/src/main/assets/web',
          emptyOutDir: true,
          rollupOptions: { input: resolvePath('./mobile.html') },
        }
      : {},
  }
})
