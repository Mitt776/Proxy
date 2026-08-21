<script lang="ts">
  // Оболочка мобильной сборки: шапка с бургером, выдвижное меню разделов и
  // область контента. Ровно та же роль, что у App.svelte на Windows, — только
  // раскладка и переключение разделов, вся логика в lib/.
  import { onMount } from "svelte";
  import {
    AddManualProfile,
    EventsOn,
    GetAppInfo,
    GetActiveProfileID,
    ListProfiles,
  } from "$api";
  import "./lib/ui.css";

  import TopBar from "./lib/mobile/TopBar.svelte";
  import Drawer from "./lib/mobile/Drawer.svelte";
  import ConnectScreenMobile from "./lib/mobile/ConnectScreenMobile.svelte";
  import Soon from "./lib/mobile/Soon.svelte";
  import type { MobileTab } from "./lib/shell/tabs";

  import { initStore, toastText } from "./lib/store";
  import { initLang, t } from "./lib/i18n";

  let version = "";
  let tab: MobileTab = "connect";
  let menuOpen = false;
  let profileName = "";
  let ready = false;

  onMount(async () => {
    const info = await GetAppInfo();
    version = info.appVersion;
    initLang(info.lang);

    await initStore();
    await importDebugLink();
    await loadProfile();
    EventsOn("profiles:changed", loadProfile);

    // Системная заставка Android держится до первого кадра WebView. Проявляем
    // интерфейс только после загрузки состояния, иначе пользователь застаёт
    // пустой экран с неактивной кнопкой и решает, что профилей нет.
    ready = true;

    // Аппаратная кнопка «назад»: сперва закрываем меню, потом уводим на
    // «Подключение». Ответ читает MainActivity — вернули false, значит свернуть
    // приложение. Ответ обязан быть синхронным: evaluateJavascript отдаёт в
    // колбэк именно возвращённое значение, промис туда не поместится.
    window.__mitmBack = () => {
      if (menuOpen) {
        menuOpen = false;
        return true;
      }
      if (tab !== "connect") {
        tab = "connect";
        return true;
      }
      return false;
    };
  });

  /**
   * Временный костыль порта: настоящего импорта профилей ещё нет (этап 3), а
   * набирать ссылку `vless://` пальцем на телефоне невозможно. Ссылка приходит
   * отладочной intent-экстрой и только в debug-сборке — см. MainActivity.startUrl().
   * Удалить вместе с параметром, когда появятся импорт из буфера и QR.
   */
  async function importDebugLink() {
    const link = new URLSearchParams(location.search).get("link");
    if (!link) return;
    // Из адреса убираем сразу: перезагрузка страницы не должна заводить дубль.
    history.replaceState(null, "", location.pathname);
    try {
      await AddManualProfile("", link);
    } catch (e) {
      console.error("отладочный импорт:", e);
    }
  }

  async function loadProfile() {
    const [list, activeId] = await Promise.all([ListProfiles(), GetActiveProfileID()]);
    const p = (list || []).find((x: { id: string }) => x.id === activeId);
    profileName = p ? p.name : "";
  }
</script>

<div class="app" class:ready>
  <TopBar bind:menuOpen />

  <main>
    {#if tab === "connect"}
      <ConnectScreenMobile hasProfile={!!profileName} />
    {:else if tab === "profiles"}
      <Soon icon="layers" title={$t("tab.profiles")} />
    {:else if tab === "routing"}
      <Soon icon="route" title={$t("tab.routing")} />
    {:else if tab === "logs"}
      <Soon icon="terminal" title={$t("tab.logs")} />
    {:else}
      <Soon icon="settings" title={$t("tab.settings")} />
    {/if}
  </main>

  <span class="version">{version}</span>
</div>

{#if menuOpen}
  <Drawer bind:tab {profileName} on:close={() => (menuOpen = false)} />
{/if}

{#if $toastText}<div class="toast">{$toastText}</div>{/if}

<style>
  .app {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    color: var(--text);
    opacity: 0;
    transition: opacity 0.35s ease;
  }
  .app.ready {
    opacity: 1;
  }

  main {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: auto;
    /* Нижний вырез — «полоска жеста»: без отступа выбор сервера и кнопки
       разделов оказываются ровно под ней и не нажимаются. */
    padding-bottom: var(--safe-bottom);
    padding-left: var(--safe-left);
    padding-right: var(--safe-right);
  }

  .version {
    position: fixed;
    right: calc(var(--s-2) + var(--safe-right));
    bottom: calc(2px + var(--safe-bottom));
    font-size: 10px;
    color: var(--muted);
    pointer-events: none;
  }

  .toast {
    position: fixed;
    left: 50%;
    bottom: calc(26px + var(--safe-bottom));
    transform: translateX(-50%);
    z-index: 400;
    background: var(--accent);
    color: #fff;
    font-size: 13px;
    font-weight: 600;
    padding: 9px 20px;
    border-radius: var(--r-pill);
    box-shadow: var(--shadow);
  }
</style>
