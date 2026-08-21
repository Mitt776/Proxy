<script lang="ts">
  // Оболочка мобильной сборки: шапка с бургером, выдвижное меню разделов и
  // область контента. Ровно та же роль, что у App.svelte на Windows, — только
  // раскладка и переключение разделов, вся логика в lib/.
  import { onMount } from "svelte";
  import {
    EventsOn,
    GetAppInfo,
    GetActiveProfileID,
    ListProfiles,
  } from "$api";
  import "./lib/ui.css";

  import { OpenURL } from "$api";
  import TopBar from "./lib/mobile/TopBar.svelte";
  import Drawer from "./lib/mobile/Drawer.svelte";
  import ConnectScreenMobile from "./lib/mobile/ConnectScreenMobile.svelte";
  import ProfilesTabMobile from "./lib/mobile/ProfilesTabMobile.svelte";
  import RulesTabMobile from "./lib/mobile/RulesTabMobile.svelte";
  import SettingsTabMobile from "./lib/mobile/SettingsTabMobile.svelte";
  // Журнал переиспользуется с Windows целиком: строки те же, фильтр работает
  // поверх кольцевого буфера в Go, а узкий экран лечится медиазапросом внутри
  // самой вкладки.
  import LogsTab from "./lib/LogsTab.svelte";
  import Icon from "./lib/icons/Icon.svelte";
  import type { MobileTab } from "./lib/shell/tabs";

  import { initStore, toastText } from "./lib/store";
  import { initLang, t } from "./lib/i18n";

  let version = "";
  let coreVersion = "";
  let dataDir = "";

  /** Найденное обновление — показывается полоской над содержимым. */
  let update: { version: string; url: string } | null = null;
  let tab: MobileTab = "connect";
  let menuOpen = false;
  let profileName = "";
  let ready = false;

  onMount(async () => {
    const info = await GetAppInfo();
    version = info.appVersion;
    coreVersion = info.coreVersion;
    dataDir = info.dataDir;
    initLang(info.lang);

    // Магазина у нас нет — о новой версии сказать больше некому. Полоска
    // появляется по событию из Go и уводит на страницу релиза.
    EventsOn("update:available", (u: { version: string; url: string }) => (update = u));

    await initStore();
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
      if (closeTopOverlay()) return true;
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
   * Закрывает верхнее модальное окно, если оно открыто.
   *
   * Ищем окно в DOM, а не ведём собственный список: модалки разбросаны по
   * компонентам, часть из них общая с десктопом (выбор ноды), и заставлять
   * каждую регистрироваться в оболочке — это ровно тот случай, когда однажды
   * забудут. Общего у всех окон ровно одно: рамка `.modal-backdrop`, которая уже
   * умеет закрываться по Escape, — им и пользуемся.
   */
  function closeTopOverlay(): boolean {
    const backdrops = document.querySelectorAll<HTMLElement>(".modal-backdrop");
    const top = backdrops[backdrops.length - 1];
    if (!top) return false;
    top.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    return true;
  }

  async function loadProfile() {
    const [list, activeId] = await Promise.all([ListProfiles(), GetActiveProfileID()]);
    const p = (list || []).find((x: { id: string }) => x.id === activeId);
    profileName = p ? p.name : "";
  }
</script>

<div class="app" class:ready>
  <TopBar bind:menuOpen />

  {#if update}
    <div class="update">
      <Icon name="download" size={15} />
      <span>{$t("m.update.ready", { v: update.version })}</span>
      <button class="go" on:click={() => OpenURL(update.url)}>{$t("m.settings.open")}</button>
      <button class="hide" aria-label={$t("common.close")} on:click={() => (update = null)}>
        <Icon name="close" size={14} />
      </button>
    </div>
  {/if}

  <main>
    {#if tab === "connect"}
      <ConnectScreenMobile hasProfile={!!profileName} />
    {:else if tab === "profiles"}
      <ProfilesTabMobile />
    {:else if tab === "routing"}
      <RulesTabMobile />
    {:else if tab === "logs"}
      <!-- Вкладка построена как каркас с растягивающимся телом (.tab-wrap), и
           ей нужна заданная высота: без обёртки список строк не прокручивался
           бы сам, а растил бы страницу. -->
      <div class="fill"><LogsTab /></div>
    {:else}
      <SettingsTabMobile appVersion={version} {coreVersion} {dataDir} />
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

  .fill {
    height: 100%;
    box-sizing: border-box;
    display: flex;
    padding: var(--s-4) var(--s-3) var(--s-3);
  }

  /* Полоска обновления — над содержимым, а не поверх него: она появляется редко
     и не должна перекрывать кнопку подключения. */
  .update {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--s-2);
    padding: 9px calc(var(--s-3) + var(--safe-right)) 9px calc(var(--s-3) + var(--safe-left));
    background: var(--accent-dim);
    border-bottom: 1px solid var(--accent);
    color: var(--accent-2);
    font-size: 12.5px;
  }
  .update span {
    flex: 1;
    min-width: 0;
  }
  .update .go {
    flex: none;
    border: none;
    border-radius: var(--r-pill);
    background: var(--accent);
    color: #fff;
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    padding: 5px 12px;
    cursor: pointer;
  }
  .update .hide {
    flex: none;
    display: inline-flex;
    border: none;
    background: transparent;
    color: inherit;
    padding: 4px;
    cursor: pointer;
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
