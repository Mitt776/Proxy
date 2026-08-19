<script lang="ts">
  // Вкладка настроек: поведение приложения, обновление подписок и выбор ядра.
  import { onMount, createEventDispatcher } from "svelte";
  import {
    GetSettings, GetAutostart, SetAutostart, SetMinimizeToTray, SetBlockQUIC,
    SetSubUpdateHours, PickCoreFile, ResetCorePath, GetAppInfo,
  } from "../../wailsjs/go/main/App";
  import { connected, reportError } from "./store";

  export let info: { coreVersion: string; coreFound: boolean; corePath: string; coreCustom: boolean; assetsDir: string; dataDir: string; state: string };

  const dispatch = createEventDispatcher<{ info: typeof info }>();

  let autostart = false;
  let minimizeToTray = true;
  let blockQUIC = true;
  let subUpdateHours = 0;

  onMount(async () => {
    try {
      const s = await GetSettings();
      minimizeToTray = s.minimizeToTray;
      blockQUIC = !s.allowQuic;
      subUpdateHours = s.subUpdateHours || 0;
      autostart = await GetAutostart();
    } catch (e) {
      reportError(e);
    }
  });

  async function save(fn: () => Promise<unknown>, rollback?: () => void) {
    try {
      await fn();
    } catch (e) {
      reportError(e);
      rollback?.();
    }
  }

  async function pickCore() {
    try {
      const ver = await PickCoreFile();
      if (ver) dispatch("info", await GetAppInfo());
    } catch (e) {
      reportError(e);
    }
  }
  async function resetCore() {
    try {
      await ResetCorePath();
      dispatch("info", await GetAppInfo());
    } catch (e) {
      reportError(e);
    }
  }

  const coreShort = (v: string) => (v ? v.replace("sing-box version ", "") : "не найдено");
</script>

<div class="wrap">
  <div class="panel-h">Приложение</div>
  <label class="check">
    <input type="checkbox" bind:checked={autostart}
           on:change={() => save(() => SetAutostart(autostart), () => (autostart = !autostart))} />
    Запускать вместе с Windows
  </label>
  <label class="check">
    <input type="checkbox" bind:checked={minimizeToTray}
           on:change={() => save(() => SetMinimizeToTray(minimizeToTray))} />
    Сворачивать в трей при закрытии окна
  </label>
  <label class="check"
         title="Заставляет браузер использовать TCP вместо HTTP-3. Нужно для TCP-нод (vless-vision, xhttp), иначе в TUN ломаются Google и YouTube.">
    <input type="checkbox" bind:checked={blockQUIC}
           on:change={() => save(() => SetBlockQUIC(blockQUIC))} />
    Резать QUIC в режиме TUN
  </label>

  <div class="panel-h">Подписки</div>
  <label class="row">
    Обновлять каждые
    <input class="fld num" type="number" min="0" max="168" bind:value={subUpdateHours}
           on:change={() => save(() => SetSubUpdateHours(subUpdateHours))} />
    часов (0 — не обновлять)
  </label>

  <div class="panel-h">Ядро</div>
  <div class="row">
    <span class="cver" title={info.corePath}>
      {coreShort(info.coreVersion)}
      {#if info.coreCustom}<span class="cbadge" title="Используется своё ядро">своё</span>{/if}
    </span>
    <button class="mini wide" on:click={pickCore} disabled={$connected}>Выбрать ядро…</button>
    {#if info.coreCustom}
      <button class="mini wide" on:click={resetCore} disabled={$connected}>Сбросить</button>
    {/if}
  </div>
  <div class="hint">
    Форк sing-box-lx нужен для нод с транспортом XHTTP — штатное ядро их не понимает.
    Смена ядра применяется при следующем подключении.
  </div>
</div>

<style>
  .wrap { display: flex; flex-direction: column; gap: 9px; overflow-y: auto; text-align: left; }
  .panel-h { margin: 8px 0 2px; }
  .panel-h:first-child { margin-top: 0; }
  .row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-2); flex-wrap: wrap; }
  .num { width: 60px; text-align: center; padding: 4px 6px; }
  .cver { font-family: ui-monospace, Consolas, monospace; font-size: 12px; color: var(--text); }
  .cbadge {
    background: #9e6a03; color: #fff; font-size: 10px; font-weight: 700;
    padding: 1px 6px; border-radius: 999px; margin-left: 4px; font-family: sans-serif;
  }
</style>
