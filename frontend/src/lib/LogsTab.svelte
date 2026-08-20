<script lang="ts">
  // Журнал ядра: живой поток строк с фильтром по уровню и подстроке.
  // Уровень ядра (что оно вообще пишет) — отдельная настройка: её смена
  // перезапускает ядро, поэтому она внизу и подписана явно.
  import { onMount } from "svelte";
  import { GetLogs, GetLogLevel, SetLogLevel } from "../../wailsjs/go/main/App";
  import { EventsOn, ClipboardSetText } from "../../wailsjs/runtime/runtime";
  import Icon from "./icons/Icon.svelte";
  import TabHead from "./shell/TabHead.svelte";
  import { showToast, reportError } from "./store";
  import { t, tr } from "./i18n";

  let logs: string[] = [];
  let box: HTMLDivElement;
  let follow = true; // прокручивать за новыми строками
  let query = "";
  let minLevel = "all";
  let coreLevel = "info";
  let levelBusy = false;

  // Порядок важен: фильтр показывает выбранный уровень и всё, что серьёзнее.
  const levels = ["trace", "debug", "info", "warn", "error"];

  onMount(async () => {
    try {
      logs = await GetLogs();
    } catch (e) { /* журнал не критичен */ }
    try {
      coreLevel = await GetLogLevel();
    } catch (e) { /* останется info */ }
    scrollDown();

    EventsOn("core:log", (line: string) => {
      logs = [...logs.slice(-1999), line];
      if (follow) queueMicrotask(scrollDown);
    });
    EventsOn("core:loglevel", (l: string) => (coreLevel = l));
  });

  // lineLevel достаёт уровень из строки ядра вида
  // "+0700 2026-08-19 21:39:19 INFO inbound/mixed[0]: tcp server started".
  // Свои строки приложения уровня не имеют — считаем их info, чтобы они не
  // пропадали при фильтрации.
  function lineLevel(line: string): string {
    const m = /\b(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)\b/.exec(line);
    if (!m) return "info";
    const l = m[1].toLowerCase();
    return l === "fatal" || l === "panic" ? "error" : l;
  }

  $: minIndex = minLevel === "all" ? -1 : levels.indexOf(minLevel);
  $: view = logs.filter((line) => {
    if (minIndex >= 0 && levels.indexOf(lineLevel(line)) < minIndex) return false;
    if (query.trim() && !line.toLowerCase().includes(query.trim().toLowerCase())) return false;
    return true;
  });
  $: hidden = logs.length - view.length;

  function scrollDown() {
    if (box) box.scrollTop = box.scrollHeight;
  }

  // Пользователь прокрутил вверх — перестаём тянуть его обратно вниз.
  function onScroll() {
    if (!box) return;
    follow = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  }

  async function copyAll() {
    try {
      await ClipboardSetText(view.join("\n"));
      showToast(hidden ? tr("logs.copiedFiltered") : tr("logs.copied"));
    } catch (e) {
      reportError(e);
    }
  }

  async function changeCoreLevel(level: string) {
    if (level === coreLevel || levelBusy) return;
    levelBusy = true;
    const prev = coreLevel;
    try {
      await SetLogLevel(level);
      coreLevel = level;
      showToast(tr("logs.levelSet", { level }));
    } catch (e) {
      coreLevel = prev;
      reportError(e);
    } finally {
      levelBusy = false;
    }
  }
</script>

<div class="tab-wrap">
  <TabHead title={$t("tab.logs")} sub={$t("logs.subtitle")}>
    <label class="check">
      <input type="checkbox" bind:checked={follow} on:change={() => follow && scrollDown()} />
      {$t("logs.follow")}
    </label>
    <button class="btn sm" on:click={copyAll}>
      <Icon name="copy" size={13} />{$t("common.copy")}
    </button>
    <button class="btn sm" on:click={() => (logs = [])}>
      <Icon name="trash" size={13} />{$t("logs.clear")}
    </button>
  </TabHead>

  <div class="bar">
    <div class="chips">
      {#each ["all", ...levels] as l}
        <button class="chip {l}" class:on={minLevel === l} on:click={() => (minLevel = l)}>
          {l === "all" ? $t("logs.all") : l}
        </button>
      {/each}
    </div>
    <input class="fld" placeholder={$t("logs.searchPh")} bind:value={query} />
  </div>

  <div class="box" bind:this={box} on:scroll={onScroll}>
    {#each view as line}
      <div class="line {lineLevel(line)}">{line}</div>
    {/each}
    {#if logs.length === 0}
      <div class="empty">{$t("logs.empty")}</div>
    {:else if view.length === 0}
      <div class="empty">{$t("logs.noMatch")}</div>
    {/if}
  </div>

  <div class="foot">
    <span class="hint grow">
      {#if hidden > 0}{$t("logs.hidden", { n: hidden })} · {/if}{$t("logs.levelHint")}
    </span>
    <span class="hint">{$t("logs.coreLevel")}</span>
    <select class="fld lvl" value={coreLevel} disabled={levelBusy}
            on:change={(e) => changeCoreLevel(e.currentTarget.value)}>
      {#each levels as l}<option value={l}>{l}</option>{/each}
    </select>
  </div>
</div>

<style>
  .bar { display: flex; align-items: center; gap: var(--s-3); flex: none; }
  .chips { display: flex; gap: 4px; flex: none; }
  .chip {
    background: var(--panel);
    border: 1px solid var(--line);
    color: var(--muted);
    border-radius: var(--r-pill);
    padding: 3px 11px;
    font: inherit;
    font-size: 11px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s, border-color 0.15s;
  }
  .chip:hover:not(.on) { color: var(--text-2); border-color: var(--line-2); }
  .chip.on { background: var(--accent-dim); border-color: var(--accent); color: var(--accent-2); font-weight: 700; }
  /* Уровни красим и в фильтре — так видно, что именно скрываешь. */
  .chip.warn.on { background: rgba(251, 191, 36, 0.14); border-color: var(--warn); color: var(--warn); }
  .chip.error.on { background: rgba(251, 113, 133, 0.14); border-color: var(--danger); color: var(--danger); }
  .bar .fld { flex: 1; min-width: 0; font-size: 12px; }

  .box {
    flex: 1;
    min-height: 80px;
    overflow-y: auto;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: var(--r-3);
    padding: 12px 14px;
    font-family: ui-monospace, Consolas, monospace;
    font-size: 11.5px;
    line-height: 1.6;
  }
  .line { white-space: pre-wrap; word-break: break-all; color: var(--text-2); }
  .line.warn { color: var(--warn); }
  .line.error { color: var(--danger); }
  .line.debug, .line.trace { color: var(--muted); }

  .foot { display: flex; align-items: center; gap: var(--s-2); flex: none; }
  .grow { flex: 1; min-width: 0; }
  .foot .lvl { flex: none; width: auto; font-size: 12px; padding: 4px 8px; }
</style>
