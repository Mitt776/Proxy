<script lang="ts">
  // Выбор приложений, которые ходят мимо туннеля. Список приносит Kotlin
  // (PackageManager), рисуем его сами — ровно так же, как на Windows устроен
  // выбор процесса в правилах: иконки приезжают готовыми data-URL.
  //
  // Системные приложения по умолчанию скрыты: их полторы сотни, среди них
  // сервисы без внятных имён, и искать в этой куче свой банк невозможно.
  import { createEventDispatcher, onMount } from "svelte";
  import { ListApps } from "$api";
  import Icon from "../icons/Icon.svelte";
  import { t } from "../i18n";
  import { errText } from "../i18n/errors";

  /** Выбранные пакеты; наружу уезжают только по «Сохранить». */
  export let selected: string[] = [];

  const dispatch = createEventDispatcher<{ save: string[]; close: void }>();

  type App = { package: string; label: string; icon: string; system: boolean };

  let apps: App[] = [];
  let loading = true;
  let error = "";
  let query = "";
  let showSystem = false;
  let picked = new Set(selected);

  onMount(async () => {
    try {
      apps = (await ListApps()) || [];
    } catch (e) {
      error = errText(e);
    } finally {
      loading = false;
    }
  });

  function toggle(pkg: string) {
    // Set не реактивен сам по себе — пересоздаём, чтобы Svelte увидел правку.
    const next = new Set(picked);
    if (next.has(pkg)) next.delete(pkg);
    else next.add(pkg);
    picked = next;
  }

  $: needle = query.trim().toLowerCase();
  $: visible = apps.filter(
    (a) =>
      (showSystem || !a.system || picked.has(a.package)) &&
      (!needle ||
        a.label.toLowerCase().includes(needle) ||
        a.package.toLowerCase().includes(needle)),
  );
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal picker" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {$t("m.apps.title")}
      <button class="icon-btn" on:click={() => dispatch("close")} aria-label={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="tools">
      <div class="search">
        <Icon name="search" size={14} />
        <input class="fld" placeholder={$t("m.apps.search")} bind:value={query} />
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={showSystem} />
        <span>{$t("m.apps.system")}</span>
      </label>
    </div>

    <div class="modal-b list">
      {#if loading}
        <div class="dim">{$t("common.loading")}</div>
      {:else if error}
        <div class="error">{error}</div>
      {:else}
        <div class="hint">{$t("m.apps.hint")}</div>
        {#each visible as a (a.package)}
          <button class="app" class:on={picked.has(a.package)} on:click={() => toggle(a.package)}>
            {#if a.icon}
              <img src={a.icon} alt="" />
            {:else}
              <span class="noicon"><Icon name="layers" size={16} /></span>
            {/if}
            <span class="txt">
              <span class="label">{a.label}</span>
              <span class="pkg">{a.package}</span>
            </span>
            <span class="box">
              {#if picked.has(a.package)}<Icon name="check" size={14} />{/if}
            </span>
          </button>
        {/each}
      {/if}
    </div>

    <div class="modal-f">
      <button class="btn" on:click={() => dispatch("close")}>{$t("common.cancel")}</button>
      <button class="btn primary" on:click={() => dispatch("save", [...picked])}>
        {$t("m.apps.save")}
      </button>
    </div>
  </div>
</div>

<style>
  .picker {
    max-height: 100%;
  }

  .tools {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    padding: var(--s-3) var(--s-4);
    border-bottom: 1px solid var(--line);
  }
  .search {
    position: relative;
    display: flex;
    align-items: center;
    color: var(--muted);
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    pointer-events: none;
  }
  .search input {
    width: 100%;
    padding-left: 30px;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
    min-height: 0;
  }
  .dim {
    color: var(--muted);
    font-size: 13px;
  }

  .app {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    padding: 9px var(--s-2);
    border: none;
    border-radius: var(--r-2);
    background: transparent;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .app.on {
    background: var(--accent-dim);
  }
  .app img,
  .app .noicon {
    width: 32px;
    height: 32px;
    flex: none;
    border-radius: 7px;
  }
  .app .noicon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--panel-2);
    color: var(--muted);
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .label {
    font-size: 14px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pkg {
    font-size: 10.5px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Галочка справа: на телефоне нажимается вся строка, а квадрат только
     показывает состояние — отдельная мишень в 20 px тут была бы издевательством. */
  .box {
    width: 22px;
    height: 22px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--line-2);
    border-radius: 6px;
    color: #fff;
  }
  .app.on .box {
    background: var(--accent);
    border-color: var(--accent);
  }
</style>
