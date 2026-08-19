<script lang="ts">
  // Оболочка приложения: шапка с состоянием, постоянная панель подключения
  // и вкладки. Вся логика живёт в компонентах lib/ — здесь только раскладка.
  import { onMount } from "svelte";
  import { GetAppInfo, GetState } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";
  import "./lib/ui.css";

  import ConnectionPanel from "./lib/ConnectionPanel.svelte";
  import ProfilesTab from "./lib/ProfilesTab.svelte";
  import RulesTab from "./lib/RulesTab.svelte";
  import ConnectionsTab from "./lib/ConnectionsTab.svelte";
  import LogsTab from "./lib/LogsTab.svelte";
  import SettingsTab from "./lib/SettingsTab.svelte";
  import { coreState, errorText, toastText } from "./lib/store";

  let info = {
    coreVersion: "", coreFound: false, corePath: "", coreCustom: false,
    assetsDir: "", dataDir: "", state: "stopped",
  };

  type Tab = "profiles" | "routing" | "connections" | "logs" | "settings";
  let tab: Tab = "profiles";
  const tabs: [Tab, string][] = [
    ["profiles", "Профили"],
    ["routing", "Маршрутизация"],
    ["connections", "Соединения"],
    ["logs", "Журнал"],
    ["settings", "Настройки"],
  ];

  const stateLabel: Record<string, string> = {
    stopped: "Отключено", starting: "Запуск…", running: "Подключено", error: "Ошибка",
  };

  onMount(async () => {
    info = await GetAppInfo();
    coreState.set(await GetState());

    EventsOn("core:state", (p: { state: string; reason: string }) => {
      coreState.set(p.state);
      if (p.reason) errorText.set(p.reason);
    });
  });
</script>

<main>
  <header>
    <h1>
      Proxy
      <span class="sub">
        на базе sing-box{info.coreVersion ? " • " + info.coreVersion.replace("sing-box version ", "") : ""}
      </span>
    </h1>
    <div class="badge {$coreState}">{stateLabel[$coreState] || $coreState}</div>
  </header>

  <div class="cols">
    <section class="panel left">
      <div class="panel-h">Подключение</div>
      <ConnectionPanel {info} />
    </section>

    <section class="panel right">
      <nav class="tabbar">
        {#each tabs as [id, label]}
          <button class:on={tab === id} on:click={() => (tab = id)}>{label}</button>
        {/each}
      </nav>

      {#if tab === "profiles"}
        <ProfilesTab />
      {:else if tab === "routing"}
        <RulesTab />
      {:else if tab === "connections"}
        <ConnectionsTab />
      {:else if tab === "logs"}
        <LogsTab />
      {:else}
        <SettingsTab {info} on:info={(e) => (info = e.detail)} />
      {/if}
    </section>
  </div>

  {#if $toastText}<div class="toast">{$toastText}</div>{/if}
</main>

<style>
  :global(body) { margin: 0; background: #0d1117; }
  main {
    font-family: "Nunito", system-ui, sans-serif;
    color: var(--text);
    padding: 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    height: calc(100vh - 32px);
    box-sizing: border-box;
  }
  header { display: flex; align-items: center; justify-content: space-between; }
  h1 { font-size: 20px; margin: 0; font-weight: 800; }
  .sub { font-size: 12px; font-weight: 400; color: var(--muted); margin-left: 6px; }

  .badge {
    padding: 5px 14px; border-radius: 999px; font-size: 13px;
    font-weight: 700; background: var(--line);
  }
  .badge.running { background: #1a7f37; }
  .badge.starting { background: #9e6a03; }
  .badge.error { background: #b62324; }

  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; flex: 1; min-height: 0; }
  .right { gap: 12px; }

  .tabbar { display: flex; gap: 6px; }
  .tabbar button {
    flex: 1; background: var(--bg); border: 1px solid var(--line); color: var(--text-2);
    padding: 6px 4px; border-radius: 6px; cursor: pointer; font-size: 12.5px; font-family: inherit;
    white-space: nowrap;
  }
  .tabbar button.on { background: #1f6feb; border-color: #1f6feb; color: #fff; font-weight: 700; }

  .toast {
    position: fixed; bottom: 22px; left: 50%; transform: translateX(-50%); z-index: 400;
    background: #1a7f37; color: #fff; font-size: 13px; font-weight: 700;
    padding: 9px 18px; border-radius: 999px; box-shadow: 0 6px 20px rgba(0, 0, 0, 0.45);
  }
</style>
