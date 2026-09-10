<script lang="ts">
  // Вкладка настроек: поведение приложения, сеть, подписки, ядро и справка.
  // Разбита на секции-карточки — каждая строка это «что делаем» слева и
  // управляющий элемент справа, без сплошного столбца галочек.
  import { onMount, createEventDispatcher } from "svelte";
  import {
    GetSettings, GetAutostart, SetAutostart, SetMinimizeToTray, SetBlockQUIC,
    SetAllowLAN, SetStrictRoute, LANProxyAddresses,
    SetSubUpdateHours, PickCoreFile, ResetCorePath, GetAppInfo,
  } from "$api";
  import Icon from "./icons/Icon.svelte";
  import TabHead from "./shell/TabHead.svelte";
  import { connected, reportError } from "./store";
  import { t, lang, setLang } from "./i18n";
  import type { Lang } from "./i18n";
  import type { AppInfo } from "./types";

  export let info: AppInfo;

  const dispatch = createEventDispatcher<{ info: typeof info }>();

  const LANGS: Lang[] = ["ru", "en"];

  let autostart = false;
  let minimizeToTray = true;
  let blockQUIC = true;
  let allowLAN = false;
  let strictRoute = true;
  let subUpdateHours = 0;
  // Адреса этой машины в локальных сетях — их вписывают на телефоне. Список
  // тянем только когда доступ открыт: пока прокси на loopback, показывать нечего.
  let lanAddrs: string[] = [];

  onMount(async () => {
    try {
      const s = await GetSettings();
      minimizeToTray = s.minimizeToTray;
      blockQUIC = !s.allowQuic;
      allowLAN = s.allowLan;
      strictRoute = !s.disableStrictRoute;
      subUpdateHours = s.subUpdateHours || 0;
      autostart = await GetAutostart();
      if (allowLAN) refreshLAN();
    } catch (e) {
      reportError(e);
    }
  });

  async function refreshLAN() {
    try {
      lanAddrs = allowLAN ? await LANProxyAddresses() : [];
    } catch (e) {
      lanAddrs = [];
      reportError(e);
    }
  }

  // Открытие прокси наружу меняет конфиг ядра, поэтому на живом соединении Go
  // перезапускает ядро. Не принял — тумблер возвращаем на место: показывать
  // включённым то, чего в работающем ядре нет, хуже, чем не включить вовсе.
  async function toggleLAN() {
    await save(
      async () => {
        await SetAllowLAN(allowLAN);
        await refreshLAN();
      },
      () => {
        allowLAN = !allowLAN;
        lanAddrs = [];
      },
    );
  }

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

  $: coreShort = info.coreVersion
    ? info.coreVersion.replace("sing-box version ", "")
    : $t("settings.coreMissing");
</script>

<div class="tab-wrap">
  <TabHead title={$t("tab.settings")} sub={$t("settings.subtitle")} />

  <div class="scroll">
    <section class="panel sec">
      <div class="sec-h"><Icon name="settings" size={14} />{$t("settings.general")}</div>

      <div class="opt">
        <div class="txt">
          <div class="ttl">{$t("settings.autostart")}</div>
          <div class="hint">{$t("settings.autostartHint")}</div>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={autostart}
                 on:change={() => save(() => SetAutostart(autostart), () => (autostart = !autostart))} />
          <span class="track"></span>
        </label>
      </div>

      <div class="opt">
        <div class="txt">
          <div class="ttl">{$t("settings.tray")}</div>
          <div class="hint">{$t("settings.trayHint")}</div>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={minimizeToTray}
                 on:change={() => save(() => SetMinimizeToTray(minimizeToTray))} />
          <span class="track"></span>
        </label>
      </div>

      <div class="opt">
        <div class="txt">
          <div class="ttl">{$t("settings.lang")}</div>
          <div class="hint">{$t("settings.langHint")}</div>
        </div>
        <div class="segmented">
          {#each LANGS as l}
            <button class:on={$lang === l} on:click={() => setLang(l)}>{l.toUpperCase()}</button>
          {/each}
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

      <div class="opt">
        <div class="txt">
          <div class="ttl">{$t("settings.lan")}</div>
          <div class="hint">{$t("settings.lanHint")}</div>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={allowLAN} on:change={toggleLAN} />
          <span class="track"></span>
        </label>
      </div>

      {#if allowLAN}
        <div class="opt sub">
          <div class="txt">
            <div class="ttl">{$t("settings.lanAddrs")}</div>
            {#if lanAddrs.length}
              <div class="addrs">
                {#each lanAddrs as a}<span class="pill mono">{a}</span>{/each}
              </div>
              <div class="hint">{$t("settings.lanAddrsHint")}</div>
            {:else}
              <div class="hint">{$t("settings.lanNone")}</div>
            {/if}
          </div>
          <button class="icon-btn" title={$t("common.refresh")} on:click={refreshLAN}>
            <Icon name="refresh" size={14} />
          </button>
        </div>
      {/if}

      <div class="opt">
        <div class="txt">
          <div class="ttl">{$t("settings.strict")}</div>
          <div class="hint">{$t("settings.strictHint")}</div>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={strictRoute}
                 on:change={() => save(
                   () => SetStrictRoute(strictRoute),
                   () => (strictRoute = !strictRoute),
                 )} />
          <span class="track"></span>
        </label>
      </div>
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
      <div class="sec-h"><Icon name="zap" size={14} />{$t("settings.core")}</div>

      <div class="opt">
        <div class="txt">
          <div class="ttl mono" title={info.corePath}>
            {coreShort}
            {#if info.coreCustom}<span class="pill warn">{$t("settings.coreCustom")}</span>{/if}
          </div>
          <div class="hint">{$t("settings.coreHint")}</div>
        </div>
        <div class="ctl">
          <button class="btn sm" on:click={pickCore} disabled={$connected}>
            {$t("settings.corePick")}
          </button>
          {#if info.coreCustom}
            <button class="btn sm" on:click={resetCore} disabled={$connected}>
              {$t("settings.coreReset")}
            </button>
          {/if}
        </div>
      </div>
    </section>

    <section class="panel sec">
      <div class="sec-h"><Icon name="info" size={14} />{$t("settings.about")}</div>

      <div class="facts">
        <span class="k">{$t("settings.version")}</span>
        <span class="v mono">{info.appVersion}</span>
        <span class="k">{$t("settings.coreVersion")}</span>
        <span class="v mono">{coreShort}</span>
        <span class="k">{$t("settings.assetsDir")}</span>
        <span class="v mono path" title={info.assetsDir}>{info.assetsDir}</span>
        <span class="k">{$t("settings.dataDir")}</span>
        <span class="v mono path" title={info.dataDir}>{info.dataDir}</span>
      </div>
      <div class="hint">{$t("settings.license")}</div>
    </section>
  </div>
</div>

<style>
  .scroll {
    display: flex;
    flex-direction: column;
    gap: var(--s-3);
    overflow-y: auto;
    min-height: 0;
    padding-bottom: var(--s-3);
  }

  /* flex: none обязателен: .panel — колонка с min-height: 0, и внутри
     прокручиваемого столбца секции ужимались, обрезая последние строки. */
  .sec { flex: none; padding: var(--s-3) var(--s-4) var(--s-4); gap: var(--s-1); }
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

  /* Строка настройки: текст слева, управление справа. Ширину текста режем,
     чтобы длинная подсказка не выдавливала тумблер за край карточки. */
  .opt {
    display: flex;
    align-items: center;
    gap: var(--s-4);
    padding: var(--s-2) 0;
  }
  .opt + .opt { border-top: 1px solid var(--line); }
  .txt { flex: 1; min-width: 0; }
  .ttl { font-size: 13px; font-weight: 600; display: flex; align-items: center; gap: 6px; }
  .txt .hint { margin-top: 2px; }
  .ctl { display: flex; align-items: center; gap: var(--s-2); flex: none; }

  /* Строка-приложение к тумблеру выше: отступ слева показывает подчинённость,
     а верхнюю границу убираем — визуально это продолжение той же настройки. */
  .opt.sub { padding-left: var(--s-3); border-top: none; }
  .addrs { display: flex; flex-wrap: wrap; gap: 6px; margin: 6px 0 4px; }
  /* Адрес — значение, а не текст подсказки: его переписывают руками в настройки
     телефона, поэтому даём его выделить мышью. */
  .addrs .pill { user-select: text; }

  .num { width: 66px; text-align: center; padding: 6px 8px; }
  .unit { font-size: 12px; color: var(--muted); }
  .mono { font-family: ui-monospace, Consolas, monospace; font-size: 12px; }

  .facts {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 5px var(--s-4);
    align-items: baseline;
    padding: var(--s-1) 0 var(--s-3);
  }
  .k { font-size: 12px; color: var(--muted); }
  .v { font-size: 12px; color: var(--text-2); }
  .path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
