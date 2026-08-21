<script lang="ts">
  // Вкладка трафика: что прямо сейчас идёт через ядро, каким маршрутом и по
  // какому правилу. Отсюда же можно оборвать соединение или сделать правило для
  // хоста/процесса, не выискивая его руками в списке правил.
  import { onMount, onDestroy } from "svelte";
  import {
    GetConnections, CloseConnection, CloseAllConnections, GetRouting, AddRule,
  } from "$api";
  import RuleEditor from "./RuleEditor.svelte";
  import Icon from "./icons/Icon.svelte";
  import TabHead from "./shell/TabHead.svelte";
  import { connected, reportError, showToast, fmtBytes } from "./store";
  import { t, tr, lang } from "./i18n";
  import { errText } from "./i18n/errors";
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
      loadError = errText(e);
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
      showToast(tr("traffic.closed"));
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
      showToast(tr("traffic.ruleAdded"));
    } catch (err) {
      reportError(err);
    }
  }

  // Возраст соединения — короткой формой, единицы зависят от языка.
  $: fmtAge = (sec: number): string => {
    const ru = $lang === "ru";
    if (sec < 60) return sec + (ru ? " с" : "s");
    if (sec < 3600) return Math.floor(sec / 60) + (ru ? " мин" : "m");
    return Math.floor(sec / 3600) + (ru ? " ч" : "h");
  };

  // Куда ушёл трафик: direct/block видно по тегу outbound-а, всё прочее — прокси.
  function outClass(out: string): string {
    if (!out) return "";
    if (out === "direct") return "direct";
    if (out === "block" || out === "reject") return "block";
    return "proxy";
  }
</script>

<div class="tab-wrap">
  <TabHead title={$t("tab.traffic")} sub={$t("traffic.subtitle")}>
    {#if rows.length}<span class="pill accent">{rows.length}</span>{/if}
    <label class="check">
      <input type="checkbox" bind:checked={paused} />
      {$t("traffic.pause")}
    </label>
    <button class="icon-btn" title={$t("common.refresh")} on:click={load} disabled={!$connected}>
      <Icon name="refresh" size={14} />
    </button>
    <button class="btn sm danger" on:click={closeAll} disabled={!$connected || !rows.length}>
      {$t("traffic.closeAll")}
    </button>
  </TabHead>

  <div class="bar">
    <span class="sico"><Icon name="search" size={15} /></span>
    <input class="fld" placeholder={$t("traffic.filterPh")} bind:value={filter} />
    <select class="fld sort" bind:value={sortBy}>
      <option value="age">{$t("traffic.sort.age")}</option>
      <option value="traffic">{$t("traffic.sort.traffic")}</option>
      <option value="host">{$t("traffic.sort.host")}</option>
    </select>
  </div>

  {#if !$connected}
    <div class="panel none"><Icon name="activity" size={28} />{$t("traffic.offline")}</div>
  {:else if loadError}
    <div class="error">{loadError}</div>
  {:else if view.length === 0}
    <div class="panel none">
      <Icon name="activity" size={28} />
      {rows.length ? $t("traffic.noMatch") : $t("traffic.quiet")}
    </div>
  {:else}
    <div class="rows">
      {#each view as r (r.id)}
        <div class="row-i conn">
          <div class="body">
            <div class="l1">
              <span class="host" title={r.destIP ? `${r.destIP}:${r.port}` : r.host}>
                {r.host || r.destIP || "?"}{r.port ? ":" + r.port : ""}
              </span>
              <span class="net">{r.network}</span>
              {#if r.process}
                <button class="proc" title={$t("traffic.procRule")}
                        on:click={() => ruleFromProcess(r)}>{r.process}</button>
              {/if}
            </div>
            <div class="l2">
              <span class="out {outClass(r.outbound)}">{r.chain || r.outbound || "—"}</span>
              <span class="rule" title={$t("traffic.ruleHint")}>{r.rule || $t("traffic.byDefault")}</span>
            </div>
          </div>

          <span class="traf tnum">
            <span class="dn">↓{$fmtBytes(r.download)}</span>
            <span class="up">↑{$fmtBytes(r.upload)}</span>
          </span>
          <span class="age tnum">{fmtAge(r.seconds)}</span>

          <button class="icon-btn" title={$t("traffic.makeRule")} on:click={() => ruleFrom(r)}>
            <Icon name="plus" size={14} />
          </button>
          <button class="icon-btn danger" title={$t("traffic.close")} on:click={() => closeOne(r)}>
            <Icon name="close" size={14} />
          </button>
        </div>
      {/each}
    </div>
  {/if}

  <div class="hint">{$t("traffic.procHint")}</div>
</div>

{#if editing}
  <RuleEditor rule={editing} {groups} on:save={saveRule} on:cancel={() => (editing = null)} />
{/if}

<style>
  .bar { display: flex; align-items: center; gap: var(--s-2); flex: none; position: relative; }
  .sico {
    position: absolute;
    left: 11px;
    display: flex;
    color: var(--muted);
    pointer-events: none;
  }
  .bar .fld { flex: 1; min-width: 0; padding-left: 34px; }
  /* Селектору нужна та же вложенность, что и правилу выше, иначе flex: 1
     перебивает фиксированную ширину и сортировка растягивается на пол-экрана. */
  .bar .sort { flex: none; width: 180px; padding-left: 12px; }

  .none {
    align-items: center;
    justify-content: center;
    gap: var(--s-3);
    padding: var(--s-6);
    color: var(--muted);
    font-size: 13px;
    flex: 1;
  }

  .conn { padding: 8px 14px; }
  .body { flex: 1; min-width: 0; }
  .l1 { display: flex; align-items: center; gap: 7px; min-width: 0; }
  .host {
    font-size: 12.5px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .net { font-size: 9.5px; color: var(--muted); text-transform: uppercase; flex: none; letter-spacing: 0.05em; }
  .proc {
    font-size: 10.5px;
    color: var(--text-2);
    background: var(--line);
    border: 0;
    border-radius: var(--r-pill);
    padding: 1px 8px;
    cursor: pointer;
    font-family: inherit;
    max-width: 150px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: none;
  }
  .proc:hover { background: var(--accent-dim); color: var(--accent-2); }

  .l2 { display: flex; align-items: center; gap: var(--s-2); font-size: 11px; color: var(--muted); margin-top: 2px; min-width: 0; }
  .out { font-weight: 700; flex: none; }
  .out.proxy { color: var(--accent-2); }
  .out.direct { color: var(--ok); }
  .out.block { color: var(--danger); }
  .rule { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Колонки трафика и возраста фиксированной ширины: иначе строки «дышат» на
     каждом опросе и список невозможно читать. */
  .traf { flex: none; width: 132px; display: flex; justify-content: flex-end; gap: var(--s-2); font-size: 11px; }
  .dn { color: var(--text-2); }
  .up { color: var(--muted); }
  .age { flex: none; width: 46px; text-align: right; font-size: 11px; color: var(--muted); }
</style>
