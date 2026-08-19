<script lang="ts">
  // Пикер запущенных процессов: имя, иконка exe и поиск. Отдаёт наверх
  // выбранные имена (chrome.exe) — именно они идут в правило.
  import { onMount, createEventDispatcher } from "svelte";
  import { ListProcesses } from "../../wailsjs/go/main/App";
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
      err = String(e);
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

<div class="overlay" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="box" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="head">
      <span class="title">Запущенные процессы</span>
      <button class="mini" title="Обновить список" on:click={reload}>⟳</button>
    </div>

    <input class="fld" placeholder="Поиск по имени…" bind:value={query} />

    {#if err}
      <div class="error">{err}</div>
    {:else if loading}
      <div class="empty">Читаю список процессов…</div>
    {:else}
      <div class="list">
        {#each shown as p (p.name)}
          <div class="row" class:on={picked.has(p.name.toLowerCase())}
               role="button" tabindex="0"
               on:click={() => toggle(p.name)}
               on:keydown={(e) => e.key === "Enter" && toggle(p.name)}>
            <span class="mark">{picked.has(p.name.toLowerCase()) ? "☑" : "☐"}</span>
            {#if p.icon}
              <img class="icon" src={p.icon} alt="" />
            {:else}
              <span class="icon ph">▪</span>
            {/if}
            <span class="name">{p.name}</span>
            <span class="path" title={p.path}>{p.path}</span>
          </div>
        {/each}
        {#if shown.length === 0}
          <div class="empty">Ничего не нашлось</div>
        {/if}
      </div>
    {/if}

    <div class="foot">
      <span class="hint">Выбрано: {picked.size}</span>
      <button class="mini wide" on:click={() => dispatch("close")}>Отмена</button>
      <button class="btn add-btn small" on:click={apply}>Добавить в правило</button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 300; background: rgba(0, 0, 0, 0.65);
    display: flex; align-items: center; justify-content: center;
  }
  .box {
    background: var(--panel); border: 1px solid var(--line-2); border-radius: 12px;
    padding: 16px; width: min(620px, 92vw); max-height: 80vh;
    display: flex; flex-direction: column; gap: 10px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.6);
  }
  .head { display: flex; align-items: center; justify-content: space-between; }
  .title { font-size: 14px; font-weight: 700; }
  .list {
    flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 3px;
    min-height: 120px;
  }
  .row {
    display: flex; align-items: center; gap: 8px; padding: 5px 8px;
    border: 1px solid transparent; border-radius: 7px; cursor: pointer; font-size: 13px;
  }
  .row:hover { background: var(--bg); }
  .row.on { border-color: var(--accent); background: var(--accent-bg); }
  .mark { color: var(--accent); width: 14px; flex: none; }
  .icon { width: 18px; height: 18px; flex: none; object-fit: contain; }
  .icon.ph { color: var(--muted-2); text-align: center; font-size: 12px; }
  .name { font-weight: 700; flex: none; }
  .path {
    color: var(--muted-2); font-size: 11px; margin-left: auto;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl;
  }
  .foot { display: flex; align-items: center; gap: 8px; }
  .foot .hint { margin-right: auto; }
  .btn.small { padding: 6px 14px; font-size: 13px; }
</style>
