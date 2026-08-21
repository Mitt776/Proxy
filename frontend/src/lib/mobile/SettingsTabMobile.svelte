<script lang="ts">
  // Настройки на телефоне. Отдельный компонент, а не ветки в общем SettingsTab:
  // из пяти десктопных секций три меняются целиком — автозапуска, трея и выбора
  // файла ядра на Android нет вовсе, зато есть приложения «мимо VPN», проверка
  // обновлений и обязательный раздел про GPLv3.
  import { onMount } from "svelte";
  import {
    CheckUpdate, GetExcludedApps, GetLogLevel, GetSettings, OpenURL,
    SetBlockQUIC, SetExcludedApps, SetLogLevel, SetSubUpdateHours, SetUpdateCheck,
  } from "$api";
  import Icon from "../icons/Icon.svelte";
  import AppPicker from "./AppPicker.svelte";
  import { connected, reportError, showToast } from "../store";
  import { t, tp, lang, setLang, tr } from "../i18n";
  import type { Lang } from "../i18n";
  import { errText } from "../i18n/errors";

  export let appVersion = "";
  export let coreVersion = "";
  export let dataDir = "";

  const LANGS: Lang[] = ["ru", "en"];
  const LOG_LEVELS = ["trace", "debug", "info", "warn", "error"];
  const SOURCES = "https://github.com/Mitt776/Proxy";

  let blockQUIC = true;
  let subUpdateHours = 0;
  let updateCheck = true;
  let logLevel = "info";
  let excluded: string[] = [];

  let checking = false;
  let updateNote = "";

  onMount(async () => {
    try {
      const s = await GetSettings();
      blockQUIC = !s.allowQuic;
      subUpdateHours = s.subUpdateHours || 0;
      updateCheck = !s.noUpdateCheck;
      logLevel = await GetLogLevel();
      excluded = (await GetExcludedApps()) || [];
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

  let pickerOpen = false;

  async function saveApps(packages: string[]) {
    pickerOpen = false;
    try {
      await SetExcludedApps(packages);
      excluded = packages;
      // Исключения задаются в момент establish(), то есть при открытии туннеля:
      // на живом соединении список сам собой не подхватится, и молчать об этом
      // нельзя — пользователь решит, что настройка не работает.
      if ($connected) showToast(tr("m.settings.appsNeedReconnect"));
    } catch (e) {
      reportError(e);
    }
  }

  async function checkNow() {
    checking = true;
    updateNote = "";
    try {
      const info = await CheckUpdate();
      updateNote = info.available
        ? tr("m.settings.updateFound", { v: info.version })
        : tr("m.settings.updateNone");
      if (info.available && info.url) lastURL = info.url;
    } catch (e) {
      updateNote = errText(e);
    } finally {
      checking = false;
    }
  }

  let lastURL = "";
</script>

<div class="wrap">
  <h1>{$t("tab.settings")}</h1>

  <section class="panel sec">
    <div class="sec-h"><Icon name="settings" size={14} />{$t("settings.general")}</div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("settings.lang")}</div>
        <div class="hint">{$t("m.settings.langHint")}</div>
      </div>
      <div class="segmented">
        {#each LANGS as l}
          <button class:on={$lang === l} on:click={() => setLang(l)}>{l.toUpperCase()}</button>
        {/each}
      </div>
    </div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("m.settings.updates")}</div>
        <div class="hint">{$t("m.settings.updatesHint")}</div>
      </div>
      <label class="toggle">
        <input type="checkbox" bind:checked={updateCheck}
               on:change={() => save(() => SetUpdateCheck(updateCheck), () => (updateCheck = !updateCheck))} />
        <span class="track"></span>
      </label>
    </div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("m.settings.checkNow")}</div>
        {#if updateNote}<div class="hint">{updateNote}</div>{/if}
      </div>
      <div class="ctl">
        {#if lastURL}
          <button class="btn sm primary" on:click={() => OpenURL(lastURL)}>
            {$t("m.settings.open")}
          </button>
        {/if}
        <button class="btn sm" on:click={checkNow} disabled={checking}>
          {checking ? $t("common.loading") : $t("common.refresh")}
        </button>
      </div>
    </div>
  </section>

  <section class="panel sec">
    <div class="sec-h"><Icon name="shield" size={14} />{$t("settings.network")}</div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("settings.quic")}</div>
        <div class="hint">{$t("settings.quicHint")}</div>
      </div>
      <label class="toggle">
        <input type="checkbox" bind:checked={blockQUIC}
               on:change={() => save(() => SetBlockQUIC(blockQUIC))} />
        <span class="track"></span>
      </label>
    </div>

    <button class="opt as-row" on:click={() => (pickerOpen = true)}>
      <div class="txt">
        <div class="ttl">{$t("m.settings.apps")}</div>
        <div class="hint">
          {excluded.length ? $tp("m.settings.appsCount", excluded.length) : $t("m.settings.appsNone")}
        </div>
      </div>
      <Icon name="chevronRight" size={16} />
    </button>
  </section>

  <section class="panel sec">
    <div class="sec-h"><Icon name="refresh" size={14} />{$t("settings.subs")}</div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("settings.subEvery")}</div>
        <div class="hint">{$t("settings.subOff")}</div>
      </div>
      <div class="ctl">
        <input class="fld num" type="number" min="0" max="168" bind:value={subUpdateHours}
               on:change={() => save(() => SetSubUpdateHours(subUpdateHours))} />
        <span class="unit">{$t("settings.subHours")}</span>
      </div>
    </div>
  </section>

  <section class="panel sec">
    <div class="sec-h"><Icon name="terminal" size={14} />{$t("tab.logs")}</div>

    <div class="opt">
      <div class="txt">
        <div class="ttl">{$t("logs.coreLevel")}</div>
        <div class="hint">{$t("logs.levelHint")}</div>
      </div>
      <select class="fld" bind:value={logLevel} on:change={() => save(() => SetLogLevel(logLevel))}>
        {#each LOG_LEVELS as l}<option value={l}>{l}</option>{/each}
      </select>
    </div>
  </section>

  <section class="panel sec">
    <div class="sec-h"><Icon name="info" size={14} />{$t("settings.about")}</div>

    <div class="facts">
      <span class="k">{$t("settings.version")}</span>
      <span class="v mono">{appVersion}</span>
      <span class="k">{$t("settings.coreVersion")}</span>
      <span class="v mono">{coreVersion.replace("sing-box version ", "")}</span>
      <span class="k">{$t("settings.dataDir")}</span>
      <span class="v mono path">{dataDir}</span>
    </div>

    <!-- Ядро линкуется в APK, поэтому Android-часть выпускается под GPLv3 и
         обязана дать пользователю ссылку на исходники. -->
    <div class="hint">{$t("m.settings.license")}</div>
    <button class="btn sm" on:click={() => OpenURL(SOURCES)}>
      <Icon name="link" size={13} />{$t("m.settings.sources")}
    </button>
  </section>
</div>

{#if pickerOpen}
  <AppPicker
    selected={excluded}
    on:save={(e) => saveApps(e.detail)}
    on:close={() => (pickerOpen = false)}
  />
{/if}

<style>
  .wrap {
    min-height: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: var(--s-3);
    padding: var(--s-4) var(--s-3) var(--s-5);
  }
  h1 {
    margin: 0;
    font-size: 19px;
    font-weight: 700;
  }

  .sec {
    flex: none;
    padding: var(--s-3) var(--s-3) var(--s-4);
    gap: var(--s-1);
  }
  .sec-h {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 11px;
    font-weight: 700;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding-bottom: var(--s-2);
  }

  .opt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s-3);
    padding: 11px 0;
    border-top: 1px solid var(--line);
  }
  .sec-h + .opt {
    border-top: none;
  }

  /* Строка-кнопка: та же разметка, что у обычной строки настройки, но целиком
     нажимается — на телефоне это привычный способ уйти на вложенный экран. */
  .as-row {
    width: 100%;
    background: transparent;
    border-left: none;
    border-right: none;
    border-bottom: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .as-row:active {
    background: var(--panel-2);
  }

  .txt {
    min-width: 0;
    flex: 1;
  }
  .ttl {
    font-size: 14px;
    font-weight: 600;
  }
  .hint {
    font-size: 11.5px;
    color: var(--muted);
    margin-top: 2px;
  }

  .ctl {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    flex: none;
  }
  .num {
    width: 66px;
    text-align: center;
  }
  .unit {
    font-size: 12px;
    color: var(--muted);
  }
  select.fld {
    width: auto;
  }

  .facts {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px var(--s-3);
    padding: var(--s-2) 0;
    font-size: 12.5px;
  }
  .k {
    color: var(--muted);
  }
  .v {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mono {
    font-family: ui-monospace, Consolas, monospace;
  }
  .path {
    word-break: break-all;
    white-space: normal;
  }

  .btn {
    align-self: flex-start;
    margin-top: var(--s-2);
  }
</style>
