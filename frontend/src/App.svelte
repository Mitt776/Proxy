<script lang="ts">
  import { onMount } from "svelte";
  import {
    Connect, Disconnect, GetAppInfo, GetLogs, GetState,
    ListProfiles, GetActiveProfileID, SetActiveProfile,
    AddManualProfile, AddSubscriptionProfile, RefreshProfile, DeleteProfile,
    GetProxies, SelectNode, TestDelay, SetRouting, GetSettings,
    GetAutostart, SetAutostart, SetMinimizeToTray,
    PickCoreFile, ResetCorePath,
  } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";
  import type { profile } from "../wailsjs/go/models";

  let info = { coreVersion: "", coreFound: false, assetsDir: "", dataDir: "", state: "stopped" };
  let state = "stopped";
  let reason = "";
  let enableTUN = false;
  let logs: string[] = [];
  let busy = false;
  let logBox: HTMLDivElement;

  let profiles: profile.Profile[] = [];
  let activeId = "";

  // маршрутизация
  let routingMode = "global";
  let blockAds = false;

  // системные настройки
  let autostart = false;
  let minimizeToTray = true;

  // ноды и статистика (Clash API)
  type NodeRow = { name: string; type: string; delay: number };
  let nodes: NodeRow[] = [];
  let nowNode = "";
  let testingAll = false;
  let stats = { downSpeed: 0, upSpeed: 0, downTotal: 0, upTotal: 0, connections: 0 };

  // форма добавления
  let addMode: "manual" | "sub" = "manual";
  let fName = "";
  let fRaw = "";
  let fURL = "";
  let addErr = "";
  let adding = false;

  const stateLabel: Record<string, string> = {
    stopped: "Отключено", starting: "Запуск…", running: "Подключено", error: "Ошибка",
  };

  onMount(async () => {
    info = await GetAppInfo();
    state = await GetState();
    logs = await GetLogs();
    await loadProfiles();
    try {
      const s = await GetSettings();
      routingMode = s.routingMode; blockAds = s.blockAds; enableTUN = s.enableTUN;
      minimizeToTray = s.minimizeToTray;
      autostart = await GetAutostart();
    } catch (e) { /* игнор */ }

    EventsOn("core:state", (p: { state: string; reason: string }) => {
      const prev = state;
      state = p.state;
      reason = p.reason || "";
      if (["stopped", "running", "error"].includes(state)) busy = false;
      if (state === "running" && prev !== "running") loadProxies();
      if (state === "stopped" || state === "error") {
        nodes = []; nowNode = "";
        stats = { downSpeed: 0, upSpeed: 0, downTotal: 0, upTotal: 0, connections: 0 };
      }
    });
    EventsOn("core:log", (line: string) => {
      logs = [...logs.slice(-1999), line];
      queueMicrotask(() => { if (logBox) logBox.scrollTop = logBox.scrollHeight; });
    });
    EventsOn("core:stats", (s: typeof stats) => { stats = s; });
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
    try { await SelectNode(name); nowNode = name; }
    catch (e) { reason = String(e); }
  }

  async function testOne(row: NodeRow) {
    try { row.delay = await TestDelay(row.name); nodes = nodes; }
    catch (e) { row.delay = 0; nodes = nodes; }
  }

  async function testAll() {
    if (testingAll) return;
    testingAll = true;
    try {
      await Promise.all(nodes.map((r) => testOne(r)));
    } finally { testingAll = false; }
  }

  async function changeRouting() {
    try { await SetRouting(routingMode, blockAds); }
    catch (e) { reason = String(e); }
  }

  async function changeAutostart() {
    try { await SetAutostart(autostart); }
    catch (e) { reason = String(e); autostart = !autostart; }
  }
  async function changeMinimize() {
    try { await SetMinimizeToTray(minimizeToTray); }
    catch (e) { reason = String(e); }
  }

  async function pickCore() {
    reason = "";
    try {
      const ver = await PickCoreFile();
      if (ver) info = await GetAppInfo();
    } catch (e) { reason = String(e); }
  }
  async function resetCore() {
    reason = "";
    try { await ResetCorePath(); info = await GetAppInfo(); }
    catch (e) { reason = String(e); }
  }

  const coreShort = (v: string) => v ? v.replace("sing-box version ", "") : "не найдено";

  async function loadProfiles() {
    profiles = await ListProfiles();
    activeId = await GetActiveProfileID();
  }

  async function add() {
    addErr = "";
    adding = true;
    try {
      if (addMode === "manual") {
        if (!fRaw.trim()) throw new Error("Вставь ссылки или JSON");
        await AddManualProfile(fName, fRaw);
      } else {
        if (!fURL.trim()) throw new Error("Укажи URL подписки");
        await AddSubscriptionProfile(fName, fURL);
      }
      fName = ""; fRaw = ""; fURL = "";
      await loadProfiles();
    } catch (e) {
      addErr = String(e);
    } finally {
      adding = false;
    }
  }

  async function activate(id: string) { await SetActiveProfile(id); activeId = id; }
  async function refresh(id: string) {
    try { await RefreshProfile(id); await loadProfiles(); }
    catch (e) { reason = String(e); }
  }
  async function remove(id: string) { await DeleteProfile(id); await loadProfiles(); }

  async function connect() {
    reason = ""; busy = true;
    try { await Connect(enableTUN); }
    catch (e) { reason = String(e); busy = false; }
  }
  async function disconnect() {
    busy = true;
    try { await Disconnect(); }
    catch (e) { reason = String(e); busy = false; }
  }

  function fmtDate(v: any): string {
    if (!v) return "";
    const d = new Date(v);
    return isNaN(d.getTime()) ? "" : d.toLocaleString();
  }

  function fmtBytes(n: number): string {
    if (!n) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
  }
  const fmtSpeed = (n: number) => fmtBytes(n) + "/s";

  function delayClass(d: number): string {
    if (!d) return "d-none";
    if (d < 200) return "d-good";
    if (d < 500) return "d-mid";
    return "d-bad";
  }
  const nodeLabel = (n: NodeRow) => n.name === "auto" ? "🔀 Авто (лучшая)" : n.name;

  $: connected = state === "running";
</script>

<main>
  <header>
    <h1>Proxy <span class="sub">на базе sing-box {info.coreVersion ? "• " + info.coreVersion.replace("sing-box version ", "") : ""}</span></h1>
    <div class="badge {state}">{stateLabel[state] || state}</div>
  </header>

  <div class="cols">
    <!-- Профили -->
    <section class="panel profiles">
      <div class="panel-h">Профили</div>

      <div class="plist">
        {#each profiles as p (p.id)}
          <div class="pcard" class:active={p.id === activeId}>
            <label class="pmain">
              <input type="radio" name="active" checked={p.id === activeId} on:change={() => activate(p.id)} disabled={connected} />
              <div class="pinfo">
                <div class="pname">{p.name}</div>
                <div class="pmeta">
                  {p.kind === "subscription" ? "подписка" : "ручной"} · {p.nodeCount} нод · {fmtDate(p.updatedAt)}
                </div>
              </div>
            </label>
            <div class="pacts">
              {#if p.kind === "subscription"}
                <button class="mini" title="Обновить" on:click={() => refresh(p.id)} disabled={connected}>⟳</button>
              {/if}
              <button class="mini danger" title="Удалить" on:click={() => remove(p.id)} disabled={connected}>✕</button>
            </div>
          </div>
        {/each}
        {#if profiles.length === 0}
          <div class="empty">Пока нет профилей. Добавь ниже ↓</div>
        {/if}
      </div>

      <div class="add">
        <div class="tabs">
          <button class:on={addMode === "manual"} on:click={() => (addMode = "manual")}>Ручной</button>
          <button class:on={addMode === "sub"} on:click={() => (addMode = "sub")}>Подписка</button>
        </div>
        <input class="fld" placeholder="Название (необязательно)" bind:value={fName} />
        {#if addMode === "manual"}
          <textarea class="fld" rows="4" spellcheck="false"
            placeholder="vless://…  vmess://…  hysteria2://…  (по одной в строке) или JSON-конфиг"
            bind:value={fRaw}></textarea>
        {:else}
          <input class="fld" placeholder="https://…/subscription" bind:value={fURL} />
        {/if}
        {#if addErr}<div class="error">{addErr}</div>{/if}
        <button class="btn add-btn" on:click={add} disabled={adding}>
          {adding ? "Добавляю…" : "Добавить профиль"}
        </button>
      </div>
    </section>

    <!-- Управление + журнал -->
    <section class="panel right">
      <div class="env">
        <span class:ok={info.coreFound} class:bad={!info.coreFound}>
          {info.coreFound ? "● ядро готово" : "● ядро не найдено"}
        </span>
        <span class="path" title={info.dataDir}>данные: {info.dataDir}</span>
      </div>

      <div class="routing">
        <label class="rmode">
          Маршрут:
          <select bind:value={routingMode} on:change={changeRouting} disabled={connected}>
            <option value="global">Весь трафик через прокси</option>
            <option value="ru-direct">РФ — напрямую (сплит-туннель)</option>
          </select>
        </label>
        <label class="ads">
          <input type="checkbox" bind:checked={blockAds} on:change={changeRouting} disabled={connected} />
          Блок рекламы
        </label>
      </div>

      <div class="sysrow">
        <label class="ads">
          <input type="checkbox" bind:checked={autostart} on:change={changeAutostart} />
          Автозапуск с Windows
        </label>
        <label class="ads">
          <input type="checkbox" bind:checked={minimizeToTray} on:change={changeMinimize} />
          Сворачивать в трей при закрытии
        </label>
      </div>

      <div class="corerow">
        <span class="clbl">Ядро:</span>
        <span class="cver" title={info.corePath}>
          {coreShort(info.coreVersion)}
          {#if info.coreCustom}<span class="cbadge" title="Используется своё ядро">своё</span>{/if}
        </span>
        <button class="mini wide" on:click={pickCore} disabled={connected}>Выбрать ядро…</button>
        {#if info.coreCustom}
          <button class="mini wide" on:click={resetCore} disabled={connected}>Сбросить</button>
        {/if}
      </div>

      <div class="conn">
        <label class="tun">
          <input type="checkbox" bind:checked={enableTUN} disabled={connected || busy} />
          Режим TUN (весь трафик, нужны права администратора)
        </label>
        {#if connected}
          <button class="btn stop" on:click={disconnect} disabled={busy}>Отключить</button>
        {:else}
          <button class="btn go" on:click={connect} disabled={busy || !info.coreFound || !activeId}>Подключить</button>
        {/if}
      </div>
      {#if reason}<div class="error">{reason}</div>{/if}

      {#if connected}
        <div class="stats">
          <div class="stat"><span class="lbl down">↓</span> {fmtSpeed(stats.downSpeed)}</div>
          <div class="stat"><span class="lbl up">↑</span> {fmtSpeed(stats.upSpeed)}</div>
          <div class="stat muted">{stats.connections} соед.</div>
          <div class="stat muted">Σ {fmtBytes(stats.downTotal + stats.upTotal)}</div>
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

      <div class="logs">
        <div class="logs-head">Журнал ядра</div>
        <div class="log-box" bind:this={logBox}>
          {#each logs as line}<div class="log-line">{line}</div>{/each}
          {#if logs.length === 0}<div class="log-empty">Пусто. Выбери профиль и нажми «Подключить».</div>{/if}
        </div>
      </div>
    </section>
  </div>
</main>

<style>
  :global(body) { margin: 0; background: #0d1117; }
  main {
    font-family: "Nunito", system-ui, sans-serif; color: #e6edf3;
    padding: 16px 20px; display: flex; flex-direction: column; gap: 14px;
    height: calc(100vh - 32px); box-sizing: border-box;
  }
  header { display: flex; align-items: center; justify-content: space-between; }
  h1 { font-size: 20px; margin: 0; font-weight: 800; }
  .sub { font-size: 12px; font-weight: 400; color: #7d8590; margin-left: 6px; }

  .badge { padding: 5px 14px; border-radius: 999px; font-size: 13px; font-weight: 700; background: #30363d; }
  .badge.running { background: #1a7f37; }
  .badge.starting { background: #9e6a03; }
  .badge.error { background: #b62324; }

  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; flex: 1; min-height: 0; }
  .panel { background: #161b22; border: 1px solid #30363d; border-radius: 10px; padding: 14px; display: flex; flex-direction: column; min-height: 0; }
  .panel-h { font-size: 13px; font-weight: 700; color: #7d8590; margin-bottom: 10px; text-transform: uppercase; letter-spacing: .04em; }

  .plist { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; min-height: 60px; }
  .pcard { display: flex; align-items: center; justify-content: space-between; gap: 8px; background: #0d1117; border: 1px solid #30363d; border-radius: 8px; padding: 8px 10px; }
  .pcard.active { border-color: #388bfd; background: #0d1a2b; }
  .pmain { display: flex; align-items: center; gap: 10px; cursor: pointer; flex: 1; min-width: 0; }
  .pinfo { min-width: 0; }
  .pname { font-weight: 700; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pmeta { font-size: 11px; color: #7d8590; }
  .pacts { display: flex; gap: 4px; flex: none; }
  .mini { background: #21262d; border: 1px solid #30363d; color: #adbac7; border-radius: 6px; width: 26px; height: 26px; cursor: pointer; font-size: 13px; }
  .mini:hover:not(:disabled) { background: #30363d; }
  .mini.danger:hover:not(:disabled) { background: #b62324; color: #fff; }
  .mini:disabled { opacity: .4; cursor: default; }
  .empty { color: #545d68; font-size: 13px; text-align: center; padding: 16px 0; }

  .add { margin-top: 12px; border-top: 1px solid #30363d; padding-top: 12px; display: flex; flex-direction: column; gap: 8px; }
  .tabs { display: flex; gap: 6px; }
  .tabs button { flex: 1; background: #0d1117; border: 1px solid #30363d; color: #adbac7; padding: 6px; border-radius: 6px; cursor: pointer; font-size: 13px; }
  .tabs button.on { background: #1f6feb; border-color: #1f6feb; color: #fff; font-weight: 700; }
  .fld { background: #0d1117; color: #e6edf3; border: 1px solid #30363d; border-radius: 6px; padding: 8px 10px; font-size: 13px; font-family: inherit; }
  textarea.fld { resize: vertical; font-family: ui-monospace, Consolas, monospace; font-size: 12px; }
  .fld:focus { outline: none; border-color: #388bfd; }

  .right { gap: 12px; }
  .env { display: flex; justify-content: space-between; align-items: center; font-size: 12px; }
  .env .ok { color: #3fb950; font-weight: 700; }
  .env .bad { color: #f85149; font-weight: 700; }
  .env .path { color: #545d68; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 60%; }

  .routing { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  .rmode { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #adbac7; }
  .rmode select { background: #0d1117; color: #e6edf3; border: 1px solid #30363d; border-radius: 6px; padding: 5px 8px; font-size: 13px; font-family: inherit; cursor: pointer; }
  .rmode select:disabled { opacity: .6; cursor: default; }
  .ads { display: flex; align-items: center; gap: 6px; font-size: 13px; cursor: pointer; color: #adbac7; }
  .sysrow { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
  .corerow { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #adbac7; flex-wrap: wrap; }
  .clbl { color: #7d8590; }
  .cver { font-family: ui-monospace, Consolas, monospace; font-size: 12px; color: #e6edf3; }
  .cbadge { background: #9e6a03; color: #fff; font-size: 10px; font-weight: 700; padding: 1px 6px; border-radius: 999px; margin-left: 4px; font-family: sans-serif; }

  .conn { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .tun { display: flex; align-items: center; gap: 8px; font-size: 13px; cursor: pointer; }

  .stats { display: flex; gap: 14px; align-items: center; background: #0d1117; border: 1px solid #30363d; border-radius: 8px; padding: 8px 12px; font-size: 13px; font-variant-numeric: tabular-nums; }
  .stat { display: flex; align-items: center; gap: 5px; font-weight: 700; }
  .stat.muted { color: #7d8590; font-weight: 400; margin-left: auto; }
  .stat.muted + .stat.muted { margin-left: 0; }
  .lbl { font-weight: 800; } .lbl.down { color: #3fb950; } .lbl.up { color: #58a6ff; }

  .nodes { display: flex; flex-direction: column; gap: 6px; min-height: 0; }
  .nodes-head { display: flex; align-items: center; justify-content: space-between; font-size: 12px; color: #7d8590; font-weight: 700; }
  .mini.wide { width: auto; padding: 0 10px; height: 24px; font-size: 12px; font-weight: 700; }
  .node-list { max-height: 150px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
  .node { display: flex; align-items: center; gap: 8px; background: #0d1117; border: 1px solid #30363d; border-radius: 7px; padding: 6px 10px; cursor: pointer; }
  .node:hover { border-color: #444c56; }
  .node.on { border-color: #388bfd; background: #0d1a2b; }
  .nsel { color: #388bfd; font-size: 12px; width: 12px; }
  .nname { flex: 1; min-width: 0; font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ndelay { font-size: 12px; font-variant-numeric: tabular-nums; padding: 2px 7px; border-radius: 999px; background: #21262d; cursor: pointer; flex: none; }
  .ndelay.d-good { color: #3fb950; } .ndelay.d-mid { color: #d29922; } .ndelay.d-bad { color: #f85149; } .ndelay.d-none { color: #545d68; }

  .btn { border: none; border-radius: 8px; padding: 9px 22px; font-size: 14px; font-weight: 700; cursor: pointer; color: #fff; }
  .btn:disabled { opacity: .5; cursor: default; }
  .btn.go { background: #238636; } .btn.go:hover:not(:disabled) { background: #2ea043; }
  .btn.stop { background: #b62324; } .btn.stop:hover:not(:disabled) { background: #d13438; }
  .add-btn { background: #1f6feb; } .add-btn:hover:not(:disabled) { background: #388bfd; }

  .error { color: #ff7b72; font-size: 12.5px; background: #21100f; border: 1px solid #5c1d1a; border-radius: 6px; padding: 7px 10px; white-space: pre-wrap; }

  .logs { display: flex; flex-direction: column; gap: 8px; flex: 1; min-height: 0; }
  .logs-head { font-size: 12px; color: #7d8590; font-weight: 700; }
  .log-box { flex: 1; min-height: 80px; overflow-y: auto; background: #0d1117; border: 1px solid #30363d; border-radius: 8px; padding: 10px; font-family: ui-monospace, Consolas, monospace; font-size: 11.5px; line-height: 1.55; }
  .log-line { white-space: pre-wrap; word-break: break-all; color: #adbac7; }
  .log-empty { color: #545d68; }
</style>
