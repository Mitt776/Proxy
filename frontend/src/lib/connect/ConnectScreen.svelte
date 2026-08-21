<script lang="ts">
  // Главный экран: флаг страны выхода, таймер сессии, 3D-кнопка, режим
  // перехвата и колонка показателей со спарклайнами.
  import { onDestroy, onMount } from "svelte";
  import {
    Connect,
    Disconnect,
    ExternalIP,
    GetMode,
    GetProxies,
    GetSettings,
    SetMode,
    TestDelay,
  } from "$api";
  import { EventsOn } from "$api";
  import worldMap from "../../assets/world-map.svg?raw";

  import Icon from "../icons/Icon.svelte";
  import Flag from "./Flag.svelte";
  import PowerButton from "./PowerButton.svelte";
  import StatCard from "./StatCard.svelte";
  import ProfilePicker from "./ProfilePicker.svelte";
  import { t } from "../i18n";
  import type { AppInfo } from "../types";
  import {
    connCount,
    connected,
    connHist,
    coreState,
    downHist,
    downSpeed,
    downTotal,
    errorText,
    fmtBytes,
    fmtDuration,
    fmtSpeed,
    latHist,
    pushHist,
    reportError,
    since,
    upHist,
    upSpeed,
    upTotal,
  } from "../store";

  export let info: AppInfo;
  export let hasProfile: boolean = false;

  let enableTUN = false;
  let busy = false;
  let mode = "Rule";
  let pickerOpen = false;

  let currentNode = "";
  let geo: { ip: string; country: string; countryCode: string; city: string } | null = null;
  let geoLoading = false;

  // Таймер считает now - since на каждом тике, а не инкрементирует счётчик:
  // в свёрнутом окне WebView2 душит таймеры, и счётчик уплыл бы на минуты.
  let now = Date.now();
  let clock: ReturnType<typeof setInterval>;
  let latency = 0;
  let latTimer: ReturnType<typeof setInterval>;

  const MODES = ["Rule", "Global", "Direct"];

  onMount(async () => {
    try {
      enableTUN = (await GetSettings()).enableTUN;
      mode = await GetMode();
    } catch (e) {
      /* дефолты сойдут */
    }

    EventsOn("core:mode", (m: string) => (mode = m));
    EventsOn("core:state", (p: { state: string }) => {
      if (["stopped", "running", "error"].includes(p.state)) busy = false;
      if (p.state === "running") {
        loadGeo();
        loadNode();
      }
      if (p.state !== "running") {
        geo = null;
        currentNode = "";
      }
    });

    clock = setInterval(() => (now = Date.now()), 1000);
    if ($connected) {
      loadGeo();
      loadNode();
    }
  });

  // Экран может быть закрыт переключением вкладки — гасим оба интервала, иначе
  // опрос задержки продолжит сорить в журнал ядра и в список соединений.
  onDestroy(() => {
    clearInterval(clock);
    clearInterval(latTimer);
  });

  // Задержку меряем только пока экран открыт и есть подключение: каждый замер —
  // это реальный HTTP-запрос ядра наружу, он попадает и в журнал, и в список
  // соединений. При уходе с экрана опрос гасится в onDestroy.
  function startLatency() {
    clearInterval(latTimer);
    measure();
    latTimer = setInterval(measure, 10000);
  }

  function stopLatency() {
    clearInterval(latTimer);
    latency = 0;
  }

  async function measure() {
    if (!$connected || !currentNode) return;
    try {
      latency = await TestDelay(currentNode);
    } catch (e) {
      latency = 0;
    }
    pushHist(latHist, latency);
  }

  // Имя выбранной ноды нужно и для замера задержки: Clash API меряет конкретный
  // outbound, а не «текущий». Ядро поднимает API с задержкой в доли секунды
  // после старта процесса, поэтому с повторами.
  async function loadNode(attempt = 0) {
    try {
      currentNode = (await GetProxies()).now;
      measure();
    } catch (e) {
      if (attempt < 5 && $connected) setTimeout(() => loadNode(attempt + 1), 700);
    }
  }

  $: if ($connected) startLatency();
  else stopLatency();

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

  async function toggle() {
    errorText.set("");
    busy = true;
    try {
      if ($connected) {
        await Disconnect();
      } else {
        await Connect(enableTUN);
      }
    } catch (e) {
      reportError(e);
      busy = false;
    }
  }

  async function changeMode(m: string) {
    const prev = mode;
    mode = m;
    try {
      await SetMode(m);
    } catch (e) {
      mode = prev;
      reportError(e);
    }
  }

  $: elapsed = $since ? now - $since : 0;
  $: canConnect = info.coreFound && hasProfile;
  $: btnState = busy && !$connected ? "starting" : $coreState;
</script>

<div class="screen">
  <!-- Фон вынесен в собственный слой с обрезкой: карта нарочно вылезает за края
       экрана, и без обрезки она растягивала бы страницу горизонтальной полосой. -->
  <div class="bg">
    <div class="map" class:live={$connected}>{@html worldMap}</div>
    <div class="halo" class:live={$connected} />
  </div>

  <div class="center">
    <div class="geo">
      <Flag code={geo?.countryCode ?? ""} size={96} />
      <div class="country">
        {#if geoLoading}
          {$t("connect.geo.loading")}
        {:else if geo}
          {geo.country || geo.countryCode}
        {:else if $connected}
          {$t("connect.geo.unknown")}
        {:else}
          {$t("connect.geo.off")}
        {/if}
      </div>
      <div class="place">
        {#if geo}
          <span>{geo.city}</span>
          <span class="ip selectable">{geo.ip}</span>
          <button class="icon-btn" title={$t("common.refresh")} on:click={() => loadGeo()}>
            <Icon name="refresh" size={12} />
          </button>
        {/if}
      </div>
    </div>

    <div class="timer">
      <div class="timer-label">{$t("connect.uptime")}</div>
      <div class="timer-value tnum" class:idle={!$since}>{fmtDuration(elapsed)}</div>
    </div>

    <PowerButton
      state={btnState}
      disabled={busy || !canConnect}
      label={$connected ? $t("connect.stop") : $t("connect.start")}
      on:click={toggle}
    />

    <div class="status">
      {#if !info.coreFound}
        <span class="bad">{$t("connect.noCore")}</span>
      {:else if !hasProfile}
        <span class="bad">{$t("connect.noProfile")}</span>
      {:else}
        <span class="dot" />
        <span>{$t("state." + $coreState)}</span>
      {/if}
    </div>

    {#if $errorText}<div class="error">{$errorText}</div>{/if}

    <div class="controls">
      <label class="toggle" title={$t("connect.tunHint")}>
        <input type="checkbox" bind:checked={enableTUN} disabled={$connected || busy} />
        <span class="track" />
        <span class="tun">
          {$t("connect.tun")}
          {#if !info.isAdmin}<span class="hint">{$t("connect.tunHint")}</span>{/if}
        </span>
      </label>

      <div class="segmented">
        {#each MODES as m}
          <button class:on={mode === m} title={$t("mode." + m + ".hint")}
                  on:click={() => changeMode(m)}>{$t("mode." + m)}</button>
        {/each}
      </div>
    </div>

    <!-- Профиль меняется и без подключения: это выбор набора нод на будущее,
         а на живом ядре App.SetActiveProfile пересоберёт конфиг на месте. -->
    <button class="server" on:click={() => (pickerOpen = true)} disabled={!hasProfile}>
      {$t("connect.changeProfile")}
      <Icon name="chevronRight" size={14} />
    </button>
  </div>

  <aside class="cards">
    <StatCard
      icon="download"
      label={$t("connect.card.down")}
      value={$fmtSpeed($downSpeed)}
      sub={$t("connect.card.session", { v: $fmtBytes($downTotal) })}
      color="var(--ok)"
      data={$downHist}
    />
    <StatCard
      icon="upload"
      label={$t("connect.card.up")}
      value={$fmtSpeed($upSpeed)}
      sub={$t("connect.card.session", { v: $fmtBytes($upTotal) })}
      color="var(--accent-2)"
      data={$upHist}
    />
    <StatCard
      icon="zap"
      label={$t("connect.card.latency")}
      value={latency ? latency + " ms" : "—"}
      sub={$connected ? $t("connect.card.every10s") : ""}
      color="var(--warn)"
      data={$latHist}
    />
    <StatCard
      icon="link"
      label={$t("connect.card.conns")}
      value={String($connCount)}
      color="var(--accent)"
      data={$connHist}
    />
  </aside>
</div>

{#if pickerOpen}
  <ProfilePicker
    on:picked={(e) => {
      // Смена профиля приходит пустым именем: какая нода стала текущей, знает
      // только ядро — перечитываем у него, а не угадываем.
      if (e.detail) {
        currentNode = e.detail;
        measure();
      } else {
        loadNode();
      }
    }}
    on:close={() => (pickerOpen = false)}
  />
{/if}

<style>
  /* Раскладка «плавающими» слоями, как на референсе: фон во всю площадь, слева
     панель разделов, справа колонка карточек, а центральный блок ровно
     посередине окна. Обычная сетка сместила бы его на половину ширины боковых
     колонок — кнопка переставала бы стоять по центру. */
  .screen {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  /* Карта и свечение — только фон, кликам не мешают. */
  .bg {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
  }
  .map {
    position: absolute;
    inset: -10% -6% auto;
    color: var(--accent);
    opacity: 0.06;
    pointer-events: none;
    transition: opacity 0.6s, color 0.6s;
  }
  .map.live {
    opacity: 0.13;
  }
  .map :global(svg) {
    width: 100%;
    height: auto;
    display: block;
  }
  .halo {
    position: absolute;
    left: 50%;
    top: 46%;
    width: 620px;
    height: 620px;
    margin: -310px 0 0 -310px;
    border-radius: 50%;
    background: radial-gradient(circle, rgba(111, 104, 144, 0.14), transparent 62%);
    pointer-events: none;
    transition: background 0.6s;
  }
  .halo.live {
    background: radial-gradient(circle, rgba(139, 92, 246, 0.22), transparent 62%);
  }

  .center {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--s-4);
    padding: var(--s-4) 0;
    /* Панель и карточки лежат поверх — центр не должен ловить клики под ними. */
    pointer-events: none;
  }
  .center > :global(*) {
    pointer-events: auto;
  }

  .geo {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--s-2);
  }
  .country {
    font-size: 17px;
    font-weight: 700;
  }
  .place {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    font-size: 11.5px;
    color: var(--muted);
    min-height: 22px;
  }
  .place .ip {
    font-family: ui-monospace, Consolas, monospace;
  }
  .place .icon-btn {
    width: 22px;
    height: 22px;
  }

  .timer {
    text-align: center;
  }
  .timer-label {
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .timer-value {
    font-size: 38px;
    font-weight: 300;
    line-height: 1.15;
    letter-spacing: 0.02em;
    color: var(--text);
    transition: color 0.3s;
  }
  .timer-value.idle {
    color: var(--line-2);
  }

  .status {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 13px;
    color: var(--text-2);
    min-height: 20px;
  }
  .status .bad {
    color: var(--warn);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: var(--s-5);
    flex-wrap: wrap;
    justify-content: center;
  }
  .tun {
    display: flex;
    flex-direction: column;
    line-height: 1.25;
  }

  .server {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    border: none;
    border-radius: var(--r-pill);
    background: var(--accent);
    color: #fff;
    font: inherit;
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    padding: 7px 16px;
    transition: background 0.15s, opacity 0.15s;
  }
  .server:hover:not(:disabled) {
    background: var(--accent-2);
  }
  .server:disabled {
    background: var(--panel-2);
    color: var(--muted);
    cursor: default;
  }

  /* Колонка карточек — самостоятельный объект у правого края, по центру
     вертикали, как и панель разделов слева. */
  .cards {
    position: absolute;
    right: 26px;
    top: 50%;
    transform: translateY(-50%);
    z-index: 5;
    width: 208px;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }
</style>
