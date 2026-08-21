<script lang="ts">
  // Оболочка приложения: своя шапка окна, сайдбар с разделами и область
  // контента. Логика живёт в компонентах lib/ — здесь только раскладка,
  // первичная загрузка и переключение разделов.
  import { onMount } from "svelte";
  import { GetAppInfo, GetActiveProfileID, ListProfiles } from "$api";
  import { EventsOn } from "$api";
  import "./lib/ui.css";

  import Splash from "./lib/shell/Splash.svelte";
  import TitleBar from "./lib/shell/TitleBar.svelte";
  import Sidebar from "./lib/shell/Sidebar.svelte";
  import type { Tab } from "./lib/shell/tabs";
  import type { AppInfo } from "./lib/types";

  import ConnectScreen from "./lib/connect/ConnectScreen.svelte";
  import ProfilesTab from "./lib/ProfilesTab.svelte";
  import RulesTab from "./lib/RulesTab.svelte";
  import ConnectionsTab from "./lib/ConnectionsTab.svelte";
  import LogsTab from "./lib/LogsTab.svelte";
  import SettingsTab from "./lib/SettingsTab.svelte";

  import { initStore, toastText } from "./lib/store";
  import { initLang } from "./lib/i18n";

  let info: AppInfo = {
    appVersion: "",
    coreVersion: "",
    coreFound: false,
    corePath: "",
    coreCustom: false,
    assetsDir: "",
    dataDir: "",
    state: "stopped",
    since: 0,
    isAdmin: false,
    lang: "ru",
    startHidden: false,
  };

  let tab: Tab = "connect";

  // Знак с заставки приземляется в этот элемент шапки.
  let markSlot: HTMLElement | null = null;
  let markVisible = false;
  let showSplash = true;

  let profileName = "";

  onMount(async () => {
    info = await GetAppInfo();
    initLang(info.lang);

    // Автозапуск в трей: окна не видно, крутить заставку не для кого — а при
    // показе из трея она бы только раздражала.
    if (info.startHidden) {
      showSplash = false;
      markVisible = true;
    }

    await initStore();
    await loadProfile();
    EventsOn("profiles:changed", loadProfile);
  });

  async function loadProfile() {
    const [list, activeId] = await Promise.all([ListProfiles(), GetActiveProfileID()]);
    const p = (list || []).find((x) => x.id === activeId);
    profileName = p ? p.name : "";
  }
</script>

<div class="app">
  <TitleBar bind:slotEl={markSlot} {markVisible} />

  <div class="body">
    <Sidebar bind:tab {profileName} isAdmin={info.isAdmin} coreFound={info.coreFound} />

    <!-- Панель висит поверх контента, поэтому отступ под неё держит сам контент.
         Экран «Подключение» его снимает: там центральный блок обязан стоять
         ровно посередине окна, а не посередине остатка после панели. -->
    <main class:bare={tab === "connect"}>
      {#if tab === "connect"}
        <ConnectScreen {info} hasProfile={!!profileName} />
      {:else if tab === "profiles"}
        <ProfilesTab />
      {:else if tab === "routing"}
        <RulesTab />
      {:else if tab === "traffic"}
        <ConnectionsTab />
      {:else if tab === "logs"}
        <LogsTab />
      {:else}
        <SettingsTab {info} on:info={(e) => (info = e.detail)} />
      {/if}

      <span class="version">{info.appVersion}</span>
    </main>
  </div>
</div>

{#if showSplash}
  <Splash
    target={markSlot}
    on:landed={() => (markVisible = true)}
    on:done={() => (showSplash = false)}
  />
{/if}

{#if $toastText}<div class="toast">{$toastText}</div>{/if}

<style>
  .app {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    color: var(--text);
  }

  .body {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  main {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    /* Слева — панель (22px отступа + 104px ширины) плюс воздух. */
    padding: var(--s-5) var(--s-5) var(--s-5) 150px;
    overflow: auto;
  }
  main.bare {
    padding: 0;
    overflow: hidden;
  }

  /* Версия приложения — мелко в правом нижнем углу области контента. */
  .version {
    position: absolute;
    right: var(--s-3);
    bottom: var(--s-2);
    font-size: 10px;
    color: var(--muted);
    pointer-events: none;
  }

  .toast {
    position: fixed;
    bottom: 26px;
    left: 50%;
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
