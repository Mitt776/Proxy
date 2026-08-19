<script lang="ts">
  // Вкладка соединений: что прямо сейчас идёт через ядро, каким маршрутом и по
  // какому правилу. Отсюда же можно оборвать соединение или сделать правило для
  // хоста/процесса, не выискивая его руками в списке правил.
  import { onMount, onDestroy } from "svelte";
  import {
    GetConnections, CloseConnection, CloseAllConnections, GetRouting, AddRule,
  } from "../../wailsjs/go/main/App";
  import RuleEditor from "./RuleEditor.svelte";
  import { connected, reportError, showToast, fmtBytes } from "./store";
  import type { main, rules } from "../../wailsjs/go/models";

  let rows: main.ConnectionRow[] = [];
  let filter = "";
  let sortBy: "age" | "traffic" | "host" = "age";
  let paused = false;
  let loadError = "";

  let groups: rules.Group[] = [];
  let editing: rules.Rule | null = null;

  let timer: ReturnType<typeof setInterval>;

  onMount(async () => {
    await load();
    try {
      groups = (await GetRouting()).groups || [];
    } catch (e) { /* группы нужны только для формы правила */ }
    // Раз в полторы секунды: чаще — мельтешит и зря дёргает Clash API.
    timer = setInterval(() => {
      if (!paused && $connected) load();
    }, 1500);
  });
  onDestroy(() => clearInterval(timer));

  async function load() {
    try {
      rows = await GetConnections();
      loadError = "";
    } catch (e) {
      loadError = String(e);
    }
  }

  $: view = rows
    .filter((r) => {
      if (!filter.trim()) return true;
      const q = filter.trim().toLowerCase();
      return [r.host, r.process, r.outbound, r.rule, r.destIP].some((v) =>
        (v || "").toLowerCase().includes(q));
    })
    .slice()
    .sort((a, b) => {
      if (sortBy === "traffic") return (b.upload + b.download) - (a.upload + a.download);
      if (sortBy === "host") return (a.host || "").localeCompare(b.host || "");
      return a.seconds - b.seconds; // самые свежие сверху
    });

  async function closeOne(r: main.ConnectionRow) {
    try {
      await CloseConnection(r.id);
      rows = rows.filter((x) => x.id !== r.id);
    } catch (e) {
      reportError(e);
    }
  }

  async function closeAll() {
    try {
      await CloseAllConnections();
      rows = [];
      showToast("Соединения оборваны");
    } catch (e) {
      reportError(e);
    }
  }

  // Правило из соединения: для домена берём его и поддомены, для голого IP —
  // адрес, для соединения без имени — процесс. Пользователю остаётся выбрать
  // действие и нажать «Сохранить».
  function ruleFrom(r: main.ConnectionRow) {
    const isIP = !r.host || r.host === r.destIP;
    let match = "domainSuffix";
    let values: string[] = [r.host];
    let name = r.host;

    if (isIP && r.destIP) {
      match = "ipCIDR";
      values = [r.destIP];
      name = r.destIP;
    }
    if (!values[0] && r.process) {
      match = "process";
      values = [r.process];
      name = r.process;
    }
    editing = {
      id: "", name, enabled: true, match, values,
      action: "proxy", target: "", method: "", tlsFragment: false, builtin: false,
    } as rules.Rule;
  }

  function ruleFromProcess(r: main.ConnectionRow) {
    editing = {
      id: "", name: r.process, enabled: true, match: "process", values: [r.process],
      action: "direct", target: "", method: "", tlsFragment: false, builtin: false,
    } as rules.Rule;
  }

  async function saveRule(e: CustomEvent<rules.Rule>) {
    const r = e.detail;
    editing = null;
    try {
      await AddRule(r);
      showToast("Правило добавлено — применено к ядру");
    } catch (err) {
      reportError(err);
    }
  }

  function fmtAge(sec: number): string {
    if (sec < 60) return sec + " с";
    if (sec < 3600) return Math.floor(sec / 60) + " мин";
    return Math.floor(sec / 3600) + " ч";
  }

  // Куда ушёл трафик: direct/block видно по тегу outbound-а, всё прочее — прокси.
  function outClass(out: string): string {
    if (!out) return "";
    if (out === "direct") return "direct";
    if (out === "block" || out === "reject") return "block";
    return "proxy";
  }
</script>

<div class="wrap">
  <div class="head">
    <span class="panel-h">Соединения{rows.length ? ` (${rows.length})` : ""}</span>
    <div class="tools">
      <label class="check">
        <input type="checkbox" bind:checked={paused} />
        Пауза
      </label>
      <button class="mini wide" on:click={load} disabled={!$connected}>⟳</button>
      <button class="mini wide danger" on:click={closeAll} disabled={!$connected || !rows.length}>
        Оборвать все
      </button>
    </div>
  </div>

  <div class="bar">
    <input class="fld" placeholder="Фильтр: домен, процесс, правило…" bind:value={filter} />
    <select class="fld sort" bind:value={sortBy}>
      <option value="age">новые сверху</option>
      <option value="traffic">по трафику</option>
      <option value="host">по имени</option>
    </select>
  </div>

  {#if !$connected}
    <div class="empty">Ядро не запущено — соединений нет.</div>
  {:else if loadError}
    <div class="error">{loadError}</div>
  {:else if view.length === 0}
    <div class="empty">
      {rows.length ? "Под фильтр ничего не подходит." : "Пока тихо. Открой сайт — соединения появятся здесь."}
    </div>
  {:else}
    <div class="list">
      {#each view as r (r.id)}
        <div class="conn">
          <div class="body">
            <div class="line1">
              <span class="host" title={r.destIP ? `${r.destIP}:${r.port}` : r.host}>
                {r.host || r.destIP || "?"}{r.port ? ":" + r.port : ""}
              </span>
              <span class="net">{r.network}</span>
              {#if r.process}
                <button class="proc" title="Правило для этой программы"
                        on:click={() => ruleFromProcess(r)}>{r.process}</button>
              {/if}
            </div>
            <div class="line2">
              <span class="out {outClass(r.outbound)}">{r.chain || r.outbound || "—"}</span>
              <span class="rule" title="Правило, которое сработало в ядре">
                {r.rule || "по умолчанию"}
              </span>
              <span class="traf">↓{fmtBytes(r.download)} ↑{fmtBytes(r.upload)}</span>
              <span class="age">{fmtAge(r.seconds)}</span>
            </div>
          </div>
          <button class="mini" title="Сделать правило" on:click={() => ruleFrom(r)}>＋</button>
          <button class="mini danger" title="Оборвать" on:click={() => closeOne(r)}>✕</button>
        </div>
      {/each}
    </div>
  {/if}

  <div class="hint">
    Имя программы ядро определяет только для соединений, попавших под правило по
    процессу, — у остальных колонка пуста.
  </div>
</div>

{#if editing}
  <RuleEditor rule={editing} {groups} on:save={saveRule} on:cancel={() => (editing = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 8px; flex: 1; min-height: 0; }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .head .panel-h { margin: 0; }
  .tools { display: flex; align-items: center; gap: 8px; }

  .bar { display: flex; gap: 6px; }
  .bar .fld { flex: 1; min-width: 0; }
  .sort { flex: none; width: 140px; }

  .list { flex: 1; min-height: 60px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
  .conn {
    display: flex; align-items: center; gap: 6px; background: var(--bg);
    border: 1px solid var(--line); border-radius: 7px; padding: 6px 8px;
  }
  .body { flex: 1; min-width: 0; text-align: left; }
  .line1 { display: flex; align-items: center; gap: 7px; min-width: 0; }
  .host {
    font-size: 12.5px; font-weight: 700; overflow: hidden;
    text-overflow: ellipsis; white-space: nowrap;
  }
  .net { font-size: 10px; color: var(--muted-2); text-transform: uppercase; flex: none; }
  .proc {
    font-size: 10.5px; color: var(--text-2); background: var(--line); border: 0;
    border-radius: 999px; padding: 1px 8px; cursor: pointer; font-family: inherit;
    max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: none;
  }
  .proc:hover { background: var(--line-2); }
  .line2 {
    display: flex; align-items: center; gap: 8px; font-size: 11px;
    color: var(--muted); margin-top: 2px; min-width: 0;
  }
  .out { font-weight: 700; flex: none; }
  .out.proxy { color: #58a6ff; }
  .out.direct { color: var(--green); }
  .out.block { color: var(--red); }
  .rule { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .traf { margin-left: auto; font-variant-numeric: tabular-nums; flex: none; }
  .age { flex: none; color: var(--muted-2); font-variant-numeric: tabular-nums; }
</style>
