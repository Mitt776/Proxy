<script lang="ts">
  // Шапка мобильной сборки. Отдельный компонент, а не ветка в shell/TitleBar:
  // у десктопной шапки задача противоположная — заменить системную рамку окна
  // (--wails-draggable, кнопки «свернуть»/«в трей»), а здесь рамку рисует сама
  // Android, зато нужен бургер и отступ под статусбар.
  import Icon from "../icons/Icon.svelte";
  import LogoMark from "../shell/LogoMark.svelte";
  import { lang, setLang, t, type Lang } from "../i18n";

  /** Открыто ли выдвижное меню — бургер превращается в крестик. */
  export let menuOpen = false;

  const langs: Lang[] = ["ru", "en"];
</script>

<header>
  <button class="burger" aria-label={menuOpen ? $t("m.menu.close") : $t("m.menu")}
          aria-expanded={menuOpen} on:click={() => (menuOpen = !menuOpen)}>
    <Icon name={menuOpen ? "close" : "menu"} size={22} stroke={1.8} />
  </button>

  <div class="brand">
    <LogoMark size={22} compact />
    <span class="name">MITM</span>
  </div>

  <div class="segmented" title={$t("titlebar.lang")}>
    {#each langs as l}
      <button class:on={$lang === l} on:click={() => setLang(l)}>{l.toUpperCase()}</button>
    {/each}
  </div>
</header>

<style>
  header {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--s-2);
    height: 52px;
    /* Вырез и «чёлка» статусбара: страница отдаётся под viewport-fit=cover,
       поэтому верх окна физически находится под системными часами. Без отступа
       бургер оказывается ровно под ними. */
    padding: var(--safe-top) calc(var(--s-2) + var(--safe-right)) 0
      calc(var(--s-2) + var(--safe-left));
    box-sizing: content-box;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }

  .burger {
    width: 40px;
    height: 40px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--r-2);
    background: transparent;
    color: var(--text);
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  .burger:active {
    background: var(--panel-2);
  }

  .brand {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }
  .name {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.18em;
    color: var(--text);
  }
</style>
