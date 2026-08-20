<script lang="ts">
  // Левая панель — отдельный «плавающий» объект: скруглённая колонка, висящая
  // по центру левого края поверх фона экрана, а не врезанная в раскладку. Из-за
  // этого её высота определяется содержимым, а центральный блок экрана
  // «Подключение» может быть выровнен по центру окна целиком.
  import Icon from "../icons/Icon.svelte";
  import type { paths } from "../icons/paths";
  import { t } from "../i18n";
  import { coreState } from "../store";
  import type { Tab } from "./tabs";
  import { TABS } from "./tabs";

  export let tab: Tab;
  export let profileName: string = "";
  export let isAdmin: boolean = false;
  export let coreFound: boolean = true;

  const icons: Record<Tab, keyof typeof paths> = {
    connect: "power",
    profiles: "layers",
    routing: "route",
    traffic: "activity",
    logs: "terminal",
    settings: "settings",
  };
</script>

<nav>
  <ul>
    {#each TABS as id}
      <li>
        <button class:on={tab === id} on:click={() => (tab = id)} title={$t("tab." + id)}>
          <Icon name={icons[id]} size={19} />
          <span>{$t("tab." + id)}</span>
        </button>
      </li>
    {/each}
  </ul>

  <!-- Имя активного профиля. Когда профиля нет, блок скрыт целиком: в 96 пикселях
       «Профиль не выбран» всё равно обрезается многоточием, а то же самое уже
       написано словами в центре экрана. -->
  {#if profileName}
    <div class="profile" title={profileName}>
      <span class="dot" class:idle={$coreState === "stopped"} class:warn={!coreFound} />
      <span class="name">{profileName}</span>
      {#if isAdmin}<span class="admin">{$t("sidebar.admin")}</span>{/if}
    </div>
  {/if}
</nav>

<style>
  nav {
    position: absolute;
    left: 22px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 5;
    /* border-box обязателен: иначе к width добавляются padding и рамка, панель
       выходит на 114px и наезжает на контент вкладок, который держит отступ
       по объявленной ширине. */
    box-sizing: border-box;
    width: 104px;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    padding: var(--s-2);
    border-radius: var(--r-4);
    background: var(--panel);
    border: 1px solid var(--line);
    box-shadow: var(--shadow);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  /* Иконка над подписью — колонка, как на референсе. */
  button {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 9px 3px;
    border: none;
    border-radius: var(--r-3);
    background: transparent;
    color: var(--text-2);
    font: inherit;
    font-size: 9.5px;
    font-weight: 500;
    line-height: 1.1;
    text-align: center;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
  }
  button span {
    display: block;
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  button:hover:not(.on) {
    background: var(--panel-2);
    color: var(--text);
  }
  /* Активный раздел — подсветка фоном и акцентный цвет: на тёмном фоне одна
     лишь смена цвета текста читается плохо. */
  button.on {
    background: var(--accent-dim);
    color: var(--accent-2);
    font-weight: 600;
  }

  .profile {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    padding: var(--s-2) 4px 4px;
    border-top: 1px solid var(--line);
    font-size: 9.5px;
    color: var(--text-2);
    min-width: 0;
  }
  /* Точка берёт --state с корня; в покое гасим её до цвета рамки. */
  .dot {
    width: 6px;
    height: 6px;
  }
  .dot.idle {
    background: var(--line-2);
  }
  .dot.warn {
    background: var(--warn);
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .admin {
    color: var(--accent-2);
    flex: none;
  }
</style>
