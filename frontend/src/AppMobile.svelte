<script lang="ts">
  // Экран проверки моста (этап 1 порта). Интерфейса здесь ещё нет — задача одна:
  // убедиться, что вызовы и события ходят между WebView и Go в обе стороны.
  // На этапе 2 этот файл станет настоящей оболочкой с бургер-меню.
  import { onMount, onDestroy } from "svelte";
  import {
    EventsOn,
    GetAppInfo,
    GetStatus,
    ListProfiles,
    GetLogs,
    AddManualProfile,
    Connect,
    Disconnect,
  } from "$api";
  import "./lib/ui.css";

  // Ссылку на ноду можно передать при запуске, чтобы не набирать её на телефоне:
  //   adb shell am start ... -d 'https://appassets.androidplatform.net/assets/web/mobile.html?link=<url-encoded>'
  // Нигде не сохраняется — только в поле ввода.
  let link = new URLSearchParams(location.search).get("link") ?? "";

  let info: any = null;
  let status: any = null;
  let profiles: any[] = [];
  let logs: string[] = [];
  let error = "";

  let offState: (() => void) | null = null;
  let offLog: (() => void) | null = null;

  async function refresh() {
    try {
      [info, status, profiles, logs] = await Promise.all([
        GetAppInfo(),
        GetStatus(),
        ListProfiles(),
        GetLogs(),
      ]);
      error = "";
      console.log(`мост: версия ${info.appVersion}, ядро ${info.coreVersion}, профилей ${profiles.length}`);
    } catch (e: any) {
      error = e?.message ?? String(e);
      console.log(`мост: ошибка — ${error}`);
    }
  }

  async function run(name: string, action: () => Promise<unknown>) {
    try {
      await action();
      error = "";
      console.log(`мост: ${name} — успех`);
    } catch (e: any) {
      error = e?.message ?? String(e);
      console.log(`мост: ${name} — ${error}`);
    }
    await refresh();
  }

  onMount(() => {
    refresh();
    offState = EventsOn("core:state", (payload: any) => {
      status = payload;
    });
    offLog = EventsOn("core:log", (line: any) => {
      logs = [...logs.slice(-200), String(line)];
    });
  });

  onDestroy(() => {
    offState?.();
    offLog?.();
  });
</script>

<main>
  <h1>MitM — проверка моста</h1>

  {#if error}
    <p class="err">{error}</p>
  {/if}

  <input class="fld" bind:value={link} placeholder="vless://…" />
  <div class="row">
    <button class="btn" on:click={() => run("AddManualProfile", () => AddManualProfile("", link))}>
      Добавить профиль
    </button>
    <button class="btn" on:click={() => run("Connect", () => Connect(true))}>Подключить</button>
    <button class="btn" on:click={() => run("Disconnect", () => Disconnect())}>Отключить</button>
    <button class="btn" on:click={refresh}>Обновить</button>
  </div>

  {#if info}
    <section>
      <h2>Окружение</h2>
      <p>версия {info.appVersion} · ядро {info.coreVersion}</p>
      <p>данные: {info.dataDir}</p>
      <p>язык: {info.lang} · платформа: {info.platform}</p>
    </section>
  {/if}

  {#if status}
    <section>
      <h2>Состояние</h2>
      <p>{status.state}{status.since ? ` · с ${new Date(status.since).toLocaleTimeString()}` : ""}</p>
    </section>
  {/if}

  <section>
    <h2>Профили ({profiles.length})</h2>
    {#each profiles as p}
      <p>{p.name} — {p.kind}</p>
    {:else}
      <p class="dim">пусто</p>
    {/each}
  </section>

  <section>
    <h2>Журнал</h2>
    <pre>{logs.slice(-20).join("\n") || "пусто"}</pre>
  </section>
</main>

<style>
  main {
    padding: 16px;
    color: var(--text);
    font: 14px/1.5 system-ui, sans-serif;
    min-height: 100vh;
    background: var(--bg);
  }
  h1 {
    font-size: 18px;
    margin: 0 0 12px;
  }
  h2 {
    font-size: 14px;
    margin: 16px 0 4px;
    color: var(--muted);
  }
  p {
    margin: 2px 0;
  }
  pre {
    white-space: pre-wrap;
    word-break: break-all;
    font-size: 11px;
    color: var(--muted);
  }
  .err {
    color: var(--danger);
  }
  .dim {
    color: var(--muted);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 8px;
  }
  .fld {
    width: 100%;
  }
</style>
