<script lang="ts">
  // Журнал ядра: живой поток строк с фильтром по уровню и подстроке.
  // Уровень ядра (что оно вообще пишет) — отдельная настройка: её смена
  // перезапускает ядро, поэтому она рядом, но подписана явно.
  import { onMount } from "svelte";
  import { GetLogs, GetLogLevel, SetLogLevel } from "../../wailsjs/go/main/App";
  import { EventsOn, ClipboardSetText } from "../../wailsjs/runtime/runtime";
  import { showToast, reportError } from "./store";

  let logs: string[] = [];
  let box: HTMLDivElement;
  let follow = true; // прокручивать за новыми строками
  let query = "";
  let minLevel = "all";
  let coreLevel = "info";
  let levelBusy = false;

  // Порядок важен: фильтр показывает выбранный уровень и всё, что серьёзнее.
  const levels = ["trace", "debug", "info", "warn", "error"];
  const levelLabels: Record<string, string> = {
    all: "все", trace: "trace", debug: "debug", info: "info", warn: "warn", error: "error",
  };

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
      showToast(hidden ? "Скопировано отфильтрованное" : "Журнал скопирован");
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
      showToast("Ядро пишет журнал на уровне " + level);
    } catch (e) {
      coreLevel = prev;
      reportError(e);
    } finally {
      levelBusy = false;
    }
  }
</script>

<div class="wrap">
  <div class="head">
    <span class="panel-h">Журнал ядра</span>
    <div class="tools">
      <label class="check">
        <input type="checkbox" bind:checked={follow} on:change={() => follow && scrollDown()} />
        Следить за концом
      </label>
      <button class="mini wide" on:click={copyAll}>Скопировать</button>
      <button class="mini wide" on:click={() => (logs = [])}>Очистить</button>
    </div>
  </div>

  <div class="bar">
    <div class="chips">
      {#each ["all", ...levels] as l}
        <button class="chip {l}" class:on={minLevel === l} on:click={() => (minLevel = l)}>
          {levelLabels[l]}
        </button>
      {/each}
    </div>
    <input class="fld" placeholder="Поиск по строке…" bind:value={query} />
  </div>

  <div class="box" bind:this={box} on:scroll={onScroll}>
    {#each view as line}
      <div class="line {lineLevel(line)}">{line}</div>
    {/each}
    {#if logs.length === 0}
      <div class="empty">Пусто. Выбери профиль и нажми «Подключить».</div>
    {:else if view.length === 0}
      <div class="empty">Под фильтр не подошла ни одна строка.</div>
    {/if}
  </div>

  <div class="foot">
    <span class="hint">
      {#if hidden > 0}Скрыто строк: {hidden}.{/if}
      Ядро пишет:
    </span>
    <select class="fld lvl" value={coreLevel} disabled={levelBusy}
            on:change={(e) => changeCoreLevel(e.currentTarget.value)}>
      {#each levels as l}<option value={l}>{l}</option>{/each}
    </select>
    <span class="hint">Смена уровня перезапускает ядро, соединение не рвётся.</span>
  </div>
</div>

<style>
  .wrap { display: flex; flex-direction: column; gap: 8px; flex: 1; min-height: 0; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .head .panel-h { margin: 0; }
  .tools { display: flex; align-items: center; gap: 8px; }

  .bar { display: flex; align-items: center; gap: 8px; }
  .chips { display: flex; gap: 4px; flex: none; }
  .chip {
    background: var(--bg); border: 1px solid var(--line); color: var(--muted);
    border-radius: 999px; padding: 2px 9px; font-size: 11px; cursor: pointer; font-family: inherit;
  }
  .chip.on { background: var(--accent-bg); border-color: var(--accent); color: var(--text); font-weight: 700; }
  .bar .fld { flex: 1; min-width: 0; font-size: 12px; }

  .box {
    flex: 1; min-height: 80px; overflow-y: auto; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 10px;
    font-family: ui-monospace, Consolas, monospace; font-size: 11.5px; line-height: 1.55;
    text-align: left;
  }
  .line { white-space: pre-wrap; word-break: break-all; color: var(--text-2); }
  .line.warn { color: var(--yellow); }
  .line.error { color: var(--red); }
  .line.debug, .line.trace { color: var(--muted); }

  .foot { display: flex; align-items: center; gap: 8px; }
  .foot .lvl { flex: none; width: auto; font-size: 12px; padding: 3px 6px; }
</style>
