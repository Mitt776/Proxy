<script lang="ts">
  // Выдвижное меню разделов — мобильная замена плавающей колонки Sidebar.
  // Колонку шириной 104 px на телефон не перенести: подписи разделов в неё не
  // помещаются, а отдавать шестую часть экрана под навигацию на 360 px нельзя.
  import { createEventDispatcher } from "svelte";
  import { fade, fly } from "svelte/transition";
  import Icon from "../icons/Icon.svelte";
  import type { paths } from "../icons/paths";
  import { t } from "../i18n";
  import { coreState } from "../store";
  import { MOBILE_TABS, type MobileTab } from "../shell/tabs";

  export let tab: MobileTab;
  export let profileName = "";

  const dispatch = createEventDispatcher<{ close: void }>();

  const icons: Record<MobileTab, keyof typeof paths> = {
    connect: "power",
    profiles: "layers",
    routing: "route",
    logs: "terminal",
    settings: "settings",
  };

  function pick(id: MobileTab) {
    tab = id;
    dispatch("close");
  }
</script>

<!-- Затемнение ловит нажатие мимо панели. Клавиатуры на телефоне нет, поэтому
     роль dialog с фокус-ловушкой тут была бы мёртвым весом. -->
<div
  class="scrim"
  transition:fade={{ duration: 150 }}
  role="button"
  tabindex="-1"
  aria-label={$t("m.menu.close")}
  on:click={() => dispatch("close")}
  on:keydown={(e) => e.key === "Escape" && dispatch("close")}
/>

<nav transition:fly={{ x: -280, duration: 190 }} aria-label={$t("m.menu")}>
  <ul>
    {#each MOBILE_TABS as id}
      <li>
        <button class:on={tab === id} on:click={() => pick(id)}>
          <Icon name={icons[id]} size={20} />
          <span>{$t("tab." + id)}</span>
        </button>
      </li>
    {/each}
  </ul>

  <!-- Активный профиль внизу: на десктопе он живёт в той же колонке разделов. -->
  <div class="profile" class:empty={!profileName}>
    <span class="dot" class:idle={$coreState === "stopped"} />
    <span class="name">{profileName || $t("m.noProfile")}</span>
  </div>
</nav>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgba(6, 4, 14, 0.62);
    border: none;
  }

  nav {
    position: fixed;
    z-index: 61;
    top: 0;
    bottom: 0;
    left: 0;
    width: min(280px, 78vw);
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    /* Панель уходит под статусбар и под «полоску жеста» — иначе при её высоте
       во весь экран содержимое упирается в системные элементы. */
    padding: calc(var(--s-3) + var(--safe-top)) var(--s-2)
      calc(var(--s-3) + var(--safe-bottom))
      calc(var(--s-2) + var(--safe-left));
    background: var(--panel);
    border-right: 1px solid var(--line);
    box-shadow: var(--shadow);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  /* Иконка слева от подписи: в отличие от десктопной колонки места хватает, а
     строка читается быстрее колонки из значков с мелкими подписями. */
  button {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--s-3);
    padding: 13px var(--s-3);
    border: none;
    border-radius: var(--r-2);
    background: transparent;
    color: var(--text-2);
    font: inherit;
    font-size: 15px;
    font-weight: 500;
    text-align: left;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  button.on {
    background: var(--accent-dim);
    color: var(--accent-2);
    font-weight: 600;
  }
  button:active:not(.on) {
    background: var(--panel-2);
  }

  .profile {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--s-2);
    padding: var(--s-3) var(--s-3) 0;
    border-top: 1px solid var(--line);
    font-size: 12px;
    color: var(--text-2);
    min-width: 0;
  }
  .profile.empty {
    color: var(--muted);
  }
  .dot {
    width: 7px;
    height: 7px;
    flex: none;
  }
  .dot.idle {
    background: var(--line-2);
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
