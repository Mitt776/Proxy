<script lang="ts">
  // Пикер запущенных процессов: имя, иконка exe и поиск. Отдаёт наверх
  // выбранные имена (chrome.exe) — именно они идут в правило.
  import { onMount, createEventDispatcher } from "svelte";
  import { ListProcesses } from "../../wailsjs/go/main/App";
  import Icon from "./icons/Icon.svelte";
  import { t } from "./i18n";
  import { errText } from "./i18n/errors";
  import type { system } from "../../wailsjs/go/models";

  export let selected: string[] = [];

  const dispatch = createEventDispatcher<{ close: void; apply: string[] }>();

  let all: system.ProcessInfo[] = [];
  let query = "";
  let loading = true;
  let err = "";
  let picked = new Set(selected.map((s) => s.toLowerCase()));

  onMount(reload);

  async function reload() {
    loading = true;
    err = "";
    try {
      all = await ListProcesses();
    } catch (e) {
      err = errText(e);
    } finally {
      loading = false;
    }
  }

  function toggle(name: string) {
    const key = name.toLowerCase();
    if (picked.has(key)) picked.delete(key);
    else picked.add(key);
    picked = picked; // реактивность Set в Svelte 3
  }

  function apply() {
    // Возвращаем имена в оригинальном регистре — так их показывает Windows.
    const names = all.filter((p) => picked.has(p.name.toLowerCase())).map((p) => p.name);
    dispatch("apply", names);
  }

  $: shown = query.trim()
    ? all.filter((p) => p.name.toLowerCase().includes(query.trim().toLowerCase()))
    : all;
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {$t("picker.title")}
      <span class="hbtns">
        <button class="icon-btn" title={$t("common.refresh")} on:click={reload}>
          <Icon name="refresh" size={15} />
        </button>
        <button class="icon-btn" title={$t("common.close")} on:click={() => dispatch("close")}>
          <Icon name="close" size={15} />
        </button>
      </span>
    </div>

    <div class="modal-b">
      <input class="fld" placeholder={$t("picker.searchPh")} bind:value={query} />

      {#if err}
        <div class="error">{err}</div>
      {:else if loading}
        <div class="empty">{$t("picker.loading")}</div>
      {:else}
        <div class="rows">
          {#each shown as p (p.name)}
            <div class="row-i proc" class:on={picked.has(p.name.toLowerCase())}
                 role="button" tabindex="0"
                 on:click={() => toggle(p.name)}
                 on:keydown={(e) => e.key === "Enter" && toggle(p.name)}>
              <span class="mark">
                {#if picked.has(p.name.toLowerCase())}<Icon name="check" size={13} />{/if}
              </span>
              {#if p.icon}
                <img class="ico" src={p.icon} alt="" />
              {:else}
                <span class="ico ph"><Icon name="folder" size={14} /></span>
              {/if}
              <span class="name">{p.name}</span>
              <span class="path" title={p.path}>{p.path}</span>
            </div>
          {/each}
          {#if shown.length === 0}
            <div class="empty">{$t("picker.empty")}</div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="modal-f">
      <button class="btn" on:click={() => dispatch("close")}>{$t("common.cancel")}</button>
      <button class="btn primary" on:click={apply} disabled={picked.size === 0}>
        {$t("picker.apply", { n: picked.size })}
      </button>
    </div>
  </div>
</div>

<style>
  .modal { max-width: 640px; }
  .hbtns { display: flex; gap: var(--s-1); }
  .modal-b { display: flex; flex-direction: column; gap: var(--s-3); }
  .rows { min-height: 160px; max-height: 46vh; }

  .proc { padding: 6px 12px; cursor: pointer; font-size: 13px; }
  .proc.on { background: var(--accent-dim); }
  .mark { width: 14px; flex: none; display: flex; color: var(--accent-2); }
  .ico { width: 18px; height: 18px; flex: none; object-fit: contain; }
  .ico.ph { display: flex; align-items: center; justify-content: center; color: var(--muted); }
  .name { font-weight: 600; flex: none; }
  .path {
    color: var(--muted);
    font-size: 11px;
    margin-left: auto;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
  }
</style>
