<script lang="ts">
  // Собственная шапка окна: у нас Frameless, системной рамки нет. Полоса помечена
  // --wails-draggable, поэтому окно таскается за неё; кнопки внутри полосы этот
  // атрибут сбрасывают, иначе клик по ним считался бы началом перетаскивания.
  import { Quit, WindowMinimise } from "$api";
  import Icon from "../icons/Icon.svelte";
  import LogoMark from "./LogoMark.svelte";
  import { lang, setLang, t, type Lang } from "../i18n";

  /** slotEl — куда приземляется знак с заставки (координаты берёт Splash). */
  export let slotEl: HTMLElement | null = null;
  /** markVisible — знак прячется, пока его не «принесёт» заставка. */
  export let markVisible: boolean = true;

  const langs: Lang[] = ["ru", "en"];
</script>

<header style="--wails-draggable:drag">
  <div class="brand">
    <span class="mark" bind:this={slotEl} class:hidden={!markVisible}>
      <LogoMark size={22} compact />
    </span>
    <span class="name">MITM</span>
  </div>

  <div class="right" style="--wails-draggable:no-drag">
    <div class="segmented" title={$t("titlebar.lang")}>
      {#each langs as l}
        <button class:on={$lang === l} on:click={() => setLang(l)}>{l.toUpperCase()}</button>
      {/each}
    </div>

    <button class="win" title={$t("titlebar.minimize")} on:click={() => WindowMinimise()}>
      <Icon name="minimize" size={16} />
    </button>
    <!-- Закрытие остаётся прежним: beforeClose в Go уводит окно в трей, а
         настоящий выход — через пункт «Выход» в меню трея. -->
    <button class="win close" title={$t("titlebar.close")} on:click={() => Quit()}>
      <Icon name="close" size={16} />
    </button>
  </div>
</header>

<style>
  header {
    height: 44px;
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--s-2) 0 var(--s-4);
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }
  .mark {
    display: block;
    transition: opacity 0.2s;
  }
  .mark.hidden {
    opacity: 0;
  }
  .name {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.18em;
    color: var(--text);
  }

  .right {
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }

  .win {
    width: 34px;
    height: 30px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--r-1);
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
  }
  .win:hover {
    background: var(--line);
    color: var(--text);
  }
  .win.close:hover {
    background: var(--danger);
    color: #fff;
  }
</style>
