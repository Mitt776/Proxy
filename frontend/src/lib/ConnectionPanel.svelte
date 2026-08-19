<script lang="ts">
  // Панель подключения: кнопка вкл/выкл, режим TUN, скорость, внешний IP
  // и список нод из Clash API.
  import { onMount } from "svelte";
  import {
    Connect, Disconnect, GetSettings, GetProxies, SelectNode,
    TestDelay, ExternalIP, GetActiveProfileID,
  } from "../../wailsjs/go/main/App";
  import { EventsOn } from "../../wailsjs/runtime/runtime";
  import { connected, errorText, reportError, fmtBytes, fmtSpeed } from "./store";

  export let info: { coreVersion: string; coreFound: boolean; corePath: string; coreCustom: boolean; assetsDir: string; dataDir: string; state: string };

  let enableTUN = false;
  let busy = false;
  let activeId = "";

  type NodeRow = { name: string; type: string; delay: number };
  let nodes: NodeRow[] = [];
  let nowNode = "";
  let testingAll = false;
  let stats = { downSpeed: 0, upSpeed: 0, downTotal: 0, upTotal: 0, connections: 0 };

  let geo: { ip: string; country: string; countryCode: string; city: string } | null = null;
  let geoLoading = false;

  onMount(async () => {
    try {
      enableTUN = (await GetSettings()).enableTUN;
      activeId = await GetActiveProfileID();
    } catch (e) { /* значения по умолчанию сойдут */ }

    EventsOn("core:state", (p: { state: string }) => {
      if (["stopped", "running", "error"].includes(p.state)) busy = false;
      if (p.state === "running") {
        loadProxies();
        loadGeo();
      }
      if (p.state === "stopped" || p.state === "error") {
        nodes = [];
        nowNode = "";
        geo = null;
        stats = { downSpeed: 0, upSpeed: 0, downTotal: 0, upTotal: 0, connections: 0 };
      }
    });
    EventsOn("core:stats", (s: typeof stats) => (stats = s));
    EventsOn("profiles:changed", async () => (activeId = await GetActiveProfileID()));

    if ($connected) {
      loadProxies();
      loadGeo();
    }
  });

  // loadProxies подтягивает список нод из Clash API (с несколькими попытками,
  // т.к. API поднимается через долю секунды после старта ядра).
  async function loadProxies(attempt = 0) {
    try {
      const v = await GetProxies();
      nodes = (v.nodes || []).map((n: any) => ({ name: n.name, type: n.type, delay: n.delay }));
      nowNode = v.now;
    } catch (e) {
      if (attempt < 5) setTimeout(() => loadProxies(attempt + 1), 700);
    }
  }

  async function selectNode(name: string) {
    try {
      await SelectNode(name);
      nowNode = name;
    } catch (e) {
      reportError(e);
    }
  }

  async function testOne(row: NodeRow) {
    try {
      row.delay = await TestDelay(row.name);
    } catch (e) {
      row.delay = 0;
    }
    nodes = nodes;
  }

  async function testAll() {
    if (testingAll) return;
    testingAll = true;
    try {
      await Promise.all(nodes.map((r) => testOne(r)));
    } finally {
      testingAll = false;
    }
  }

  // loadGeo с ретраями: сразу после подключения нода (Reality-хендшейк) может
  // быть ещё не готова — первые запросы падают, поэтому повторяем.
  async function loadGeo(attempt = 0) {
    if (attempt === 0) geoLoading = true;
    try {
      geo = await ExternalIP();
      geoLoading = false;
    } catch (e) {
      if (attempt < 4 && $connected) {
        setTimeout(() => loadGeo(attempt + 1), 1500);
      } else {
        geo = null;
        geoLoading = false;
      }
    }
  }

  function flagEmoji(cc: string): string {
    if (!cc || cc.length !== 2) return "🌐";
    return String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
  }

  async function connect() {
    errorText.set("");
    busy = true;
    try {
      await Connect(enableTUN);
    } catch (e) {
      reportError(e);
      busy = false;
    }
  }
  async function disconnect() {
    busy = true;
    try {
      await Disconnect();
    } catch (e) {
      reportError(e);
      busy = false;
    }
  }

  function delayClass(d: number): string {
    if (!d) return "d-none";
    if (d < 200) return "d-good";
    if (d < 500) return "d-mid";
    return "d-bad";
  }
  const nodeLabel = (n: NodeRow) => (n.name === "auto" ? "🔀 Авто (лучшая)" : n.name);
</script>

<div class="wrap">
  <div class="env">
    <span class:ok={info.coreFound} class:bad={!info.coreFound}>
      {info.coreFound ? "● ядро готово" : "● ядро не найдено"}
    </span>
    <span class="path" title={info.dataDir}>данные: {info.dataDir}</span>
  </div>

  <div class="conn">
    <label class="check">
      <input type="checkbox" bind:checked={enableTUN} disabled={$connected || busy} />
      Режим TUN (весь трафик, нужны права администратора)
    </label>
    {#if $connected}
      <button class="btn stop" on:click={disconnect} disabled={busy}>Отключить</button>
    {:else}
      <button class="btn go" on:click={connect} disabled={busy || !info.coreFound || !activeId}>
        Подключить
      </button>
    {/if}
  </div>

  {#if $errorText}<div class="error">{$errorText}</div>{/if}

  {#if $connected}
    <div class="stats">
      <div class="stat"><span class="lbl down">↓</span> {fmtSpeed(stats.downSpeed)}</div>
      <div class="stat"><span class="lbl up">↑</span> {fmtSpeed(stats.upSpeed)}</div>
      <div class="stat muted">{stats.connections} соед.</div>
      <div class="stat muted">Σ {fmtBytes(stats.downTotal + stats.upTotal)}</div>
    </div>

    <div class="geo">
      {#if geoLoading}
        <span class="geo-load">Определяю внешний IP…</span>
      {:else if geo}
        <span class="geo-flag">{flagEmoji(geo.countryCode)}</span>
        <span class="geo-loc">{geo.country || geo.countryCode}{geo.city ? ", " + geo.city : ""}</span>
        <span class="geo-ip">{geo.ip}</span>
      {:else}
        <span class="geo-load">IP не определён</span>
      {/if}
      <button class="mini" title="Обновить" on:click={() => loadGeo()} disabled={geoLoading}>⟳</button>
    </div>

    {#if nodes.length}
      <div class="nodes">
        <div class="nodes-head">
          <span>Ноды ({nodes.length})</span>
          <button class="mini wide" on:click={testAll} disabled={testingAll} title="Пинг всех нод">
            {testingAll ? "тест…" : "⚡ Тест всех"}
          </button>
        </div>
        <div class="node-list">
          {#each nodes as n (n.name)}
            <div class="node" class:on={n.name === nowNode}
                 role="button" tabindex="0" on:click={() => selectNode(n.name)}
                 on:keydown={(e) => e.key === "Enter" && selectNode(n.name)}>
              <span class="nsel">{n.name === nowNode ? "●" : "○"}</span>
              <span class="nname" title={n.name}>{nodeLabel(n)}</span>
              <span class="ndelay {delayClass(n.delay)}"
                    role="button" tabindex="0" title="Проверить задержку"
                    on:click|stopPropagation={() => testOne(n)}
                    on:keydown|stopPropagation={(e) => e.key === "Enter" && testOne(n)}>
                {n.delay ? n.delay + " ms" : "—"}
              </span>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .wrap { display: flex; flex-direction: column; gap: 12px; min-height: 0; }
  .env { display: flex; justify-content: space-between; align-items: center; font-size: 12px; }
  .env .ok { color: var(--green); font-weight: 700; }
  .env .bad { color: var(--red); font-weight: 700; }
  .env .path {
    color: var(--muted-2); overflow: hidden; text-overflow: ellipsis;
    white-space: nowrap; max-width: 60%;
  }

  .conn { display: flex; align-items: center; justify-content: space-between; gap: 10px; }

  .stats {
    display: flex; gap: 14px; align-items: center; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 8px 12px;
    font-size: 13px; font-variant-numeric: tabular-nums;
  }
  .stat { display: flex; align-items: center; gap: 5px; font-weight: 700; }
  .stat.muted { color: var(--muted); font-weight: 400; margin-left: auto; }
  .stat.muted + .stat.muted { margin-left: 0; }
  .lbl { font-weight: 800; }
  .lbl.down { color: var(--green); }
  .lbl.up { color: #58a6ff; }

  .geo {
    display: flex; align-items: center; gap: 8px; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 7px 12px; font-size: 13px;
  }
  .geo-flag { font-size: 16px; }
  .geo-loc { font-weight: 700; }
  .geo-ip {
    color: var(--muted); font-family: ui-monospace, Consolas, monospace;
    font-size: 12px; margin-left: auto;
  }
  .geo-load { color: var(--muted); }
  .geo .mini { width: 24px; height: 24px; }

  .nodes { display: flex; flex-direction: column; gap: 6px; min-height: 0; }
  .nodes-head {
    display: flex; align-items: center; justify-content: space-between;
    font-size: 12px; color: var(--muted); font-weight: 700;
  }
  .node-list { max-height: 190px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
  .node {
    display: flex; align-items: center; gap: 8px; background: var(--bg);
    border: 1px solid var(--line); border-radius: 7px; padding: 6px 10px; cursor: pointer;
  }
  .node:hover { border-color: var(--line-2); }
  .node.on { border-color: var(--accent); background: var(--accent-bg); }
  .nsel { color: var(--accent); font-size: 12px; width: 12px; }
  .nname {
    flex: 1; min-width: 0; font-size: 13px; white-space: nowrap;
    overflow: hidden; text-overflow: ellipsis; text-align: left;
  }
  .ndelay {
    font-size: 12px; font-variant-numeric: tabular-nums; padding: 2px 7px;
    border-radius: 999px; background: #21262d; cursor: pointer; flex: none;
  }
  .ndelay.d-good { color: var(--green); }
  .ndelay.d-mid { color: var(--yellow); }
  .ndelay.d-bad { color: var(--red); }
  .ndelay.d-none { color: var(--muted-2); }
</style>
