<script lang="ts">
  // Смена профиля поверх экрана подключения — и, если ядро живо, выбор ноды
  // внутри профиля. Две сущности разные, поэтому разведены по переключателю:
  // профиль это набор нод целиком, нода — одна точка выхода внутри него.
  //
  // Профиль меняется и на работающем соединении: App.SetActiveProfile
  // пересобирает конфиг и перезапускает ядро, не снимая системный прокси.
  import { createEventDispatcher, onMount } from "svelte";
  import {
    GetProxies,
    ListProfiles,
    SelectNode,
    SetActiveProfile,
    TestDelay,
    GetActiveProfileID,
  } from "$api";
  import Icon from "../icons/Icon.svelte";
  import { t, tp } from "../i18n";
  import { connected, reportError } from "../store";

  const dispatch = createEventDispatcher<{ close: void; picked: string }>();

  type NodeRow = { name: string; type: string; delay: number };
  type ProfileRow = { id: string; name: string; nodeCount: number };

  let view: "profiles" | "nodes" = "profiles";

  let nodes: NodeRow[] = [];
  let now = "";
  let query = "";
  let testingAll = false;
  let loading = true;

  let profiles: ProfileRow[] = [];
  let activeId = "";
  let switching = "";

  onMount(async () => {
    await loadProfiles();
    await load();
  });

  async function loadProfiles() {
    try {
      const list = await ListProfiles();
      profiles = (list || []).map((p) => ({ id: p.id, name: p.name, nodeCount: p.nodeCount }));
      activeId = await GetActiveProfileID();
    } catch (e) {
      /* без профилей модалка всё равно покажет список нод живого ядра */
    }
  }

  async function load() {
    loading = true;
    try {
      const v = await GetProxies();
      nodes = (v.nodes || []).map((n: any) => ({ name: n.name, type: n.type, delay: n.delay }));
      now = v.now;
    } catch (e) {
      nodes = [];
    }
    loading = false;
  }

  async function pick(name: string) {
    try {
      await SelectNode(name);
      now = name;
      dispatch("picked", name);
      dispatch("close");
    } catch (e) {
      reportError(e);
    }
  }

  // Профиль применяется на месте: пользователь должен увидеть галочку и новый
  // список нод, а не гадать, подхватилось ли. Поэтому модалка не закрывается.
  async function changeProfile(id: string) {
    if (id === activeId || switching) return;
    switching = id;
    try {
      await SetActiveProfile(id);
      activeId = id;
      await load();
      dispatch("picked", "");
    } catch (e) {
      reportError(e);
    } finally {
      switching = "";
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
      await Promise.all(shown.map((r) => testOne(r)));
    } finally {
      testingAll = false;
    }
  }

  function delayClass(d: number): string {
    if (!d) return "none";
    if (d < 200) return "good";
    if (d < 500) return "mid";
    return "bad";
  }

  $: shown = nodes.filter((n) => n.name.toLowerCase().includes(query.trim().toLowerCase()));
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div class="modal-backdrop" on:click|self={() => dispatch("close")}>
  <div class="modal">
    <div class="modal-h">
      <span>{$t("connect.picker.title")}</span>
      <button class="icon-btn" on:click={() => dispatch("close")} title={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="tools">
      <div class="segmented">
        <button class:on={view === "profiles"} on:click={() => (view = "profiles")}>
          {$t("tab.profiles")}
        </button>
        <button class:on={view === "nodes"} on:click={() => (view = "nodes")}>
          {$t("connect.picker.nodes")}
        </button>
      </div>

      {#if view === "nodes"}
        <div class="search">
          <Icon name="search" size={14} />
          <input class="fld" bind:value={query} placeholder={$t("common.search")} />
        </div>
        <button class="btn sm" on:click={testAll} disabled={testingAll || !shown.length}>
          <Icon name="zap" size={13} />
          {testingAll ? $t("connect.picker.testing") : $t("connect.picker.testAll")}
        </button>
      {/if}
    </div>

    <div class="modal-b list">
      {#if view === "profiles"}
        {#if !profiles.length}
          <div class="empty">{$t("profiles.empty")}</div>
        {:else}
          {#each profiles as p (p.id)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <div class="node" class:on={p.id === activeId} class:busy={switching === p.id}
                 on:click={() => changeProfile(p.id)}>
              <span class="mark">
                {#if p.id === activeId}<Icon name="check" size={14} />{/if}
              </span>
              <span class="name" title={p.name}>{p.name}</span>
              <span class="cnt">{$tp("profiles.nodes", p.nodeCount)}</span>
            </div>
          {/each}
        {/if}
      {:else if loading}
        <div class="empty">{$t("common.loading")}</div>
      {:else if !$connected}
        <div class="empty">{$t("connect.picker.needConnection")}</div>
      {:else if !shown.length}
        <div class="empty">{$t("connect.picker.empty")}</div>
      {:else}
        {#each shown as n (n.name)}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <div class="node" class:on={n.name === now} on:click={() => pick(n.name)}>
            <span class="mark">
              {#if n.name === now}<Icon name="check" size={14} />{/if}
            </span>
            <span class="name" title={n.name}>
              {n.name === "auto" ? $t("connect.picker.auto") : n.name}
            </span>
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <span
              class="delay tnum {delayClass(n.delay)}"
              title={$t("connect.picker.testOne")}
              on:click|stopPropagation={() => testOne(n)}
            >
              {n.delay ? n.delay + " ms" : "—"}
            </span>
          </div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .modal {
    max-width: 460px;
    max-height: 78vh;
  }

  .tools {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    padding: var(--s-3) var(--s-5);
    border-bottom: 1px solid var(--line);
  }
  .search {
    position: relative;
    flex: 1;
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
    gap: 4px;
    padding: var(--s-3) var(--s-4);
  }

  .node {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-2);
    background: var(--bg);
    cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
  }
  .node:hover {
    border-color: var(--line-2);
    background: var(--panel-2);
  }
  .node.on {
    border-color: var(--accent);
    background: var(--accent-dim);
  }
  .node.busy {
    opacity: 0.55;
    cursor: progress;
  }
  .mark {
    width: 14px;
    display: flex;
    color: var(--accent-2);
    flex: none;
  }
  .name {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cnt {
    font-size: 11px;
    color: var(--muted);
    flex: none;
  }
  .delay {
    font-size: 11.5px;
    font-weight: 600;
    padding: 3px 9px;
    border-radius: var(--r-pill);
    background: var(--line);
    flex: none;
    cursor: pointer;
  }
  .delay.good { color: var(--ok); }
  .delay.mid { color: var(--warn); }
  .delay.bad { color: var(--danger); }
  .delay.none { color: var(--muted); }
</style>
