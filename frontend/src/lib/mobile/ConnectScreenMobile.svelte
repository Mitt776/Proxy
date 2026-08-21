<script lang="ts">
  // Экран «Подключение» на телефоне. Отдельный компонент, а не ветки внутри
  // connect/ConnectScreen.svelte: там раскладка построена вокруг трёх колонок
  // (панель разделов, центр, карточки показателей), а здесь колонка одна и
  // карточек нет вовсе — общего осталось бы меньше, чем расхождений.
  //
  // Переиспользуются, наоборот, все тяжёлые части: Flag, PowerButton,
  // ProfilePicker, сторы и словари — те же самые.
  import { onDestroy, onMount } from "svelte";
  import {
    Connect,
    Disconnect,
    ExternalIP,
    GetMode,
    GetProxies,
    SetMode,
    TestDelay,
  } from "$api";
  import { EventsOn } from "$api";

  import Icon from "../icons/Icon.svelte";
  import Flag from "../connect/Flag.svelte";
  import PowerButton from "../connect/PowerButton.svelte";
  import ProfilePicker from "../connect/ProfilePicker.svelte";
  import { t } from "../i18n";
  import { connected, coreState, errorText, fmtDuration, reportError, since } from "../store";

  export let hasProfile = false;

  let busy = false;
  let mode = "Rule";
  let pickerOpen = false;

  let currentNode = "";
  let geo: { ip: string; country: string; countryCode: string; city: string } | null = null;
  let geoLoading = false;

  // Таймер считает now - since на каждом тике, а не крутит собственный счётчик:
  // в свёрнутом приложении WebView душит таймеры, и счётчик отстал бы на минуты.
  // На Android это тем более заметно — туннель живёт в фоне часами.
  let now = Date.now();
  let clock: ReturnType<typeof setInterval>;

  let latency = 0;
  let latTimer: ReturnType<typeof setInterval>;

  // Диаметр кнопки — от высоты экрана: на 640-пиксельном телефоне фиксированные
  // 200 px не оставляют места режиму и выбору сервера, на планшете теряются.
  let viewportH = 0;
  $: buttonSize = Math.round(Math.min(200, Math.max(132, viewportH * 0.24)));

  const MODES = ["Rule", "Global", "Direct"];

  onMount(async () => {
    try {
      mode = await GetMode();
    } catch (e) {
      /* дефолт сойдёт */
    }

    EventsOn("core:mode", (m: string) => (mode = m));
    EventsOn("core:state", (p: { state: string }) => {
      if (["stopped", "running", "error"].includes(p.state)) busy = false;
      if (p.state === "running") {
        loadGeo();
        loadNode();
      } else {
        geo = null;
        currentNode = "";
      }
    });

    clock = setInterval(() => (now = Date.now()), 1000);
    document.addEventListener("visibilitychange", onVisibility);

    if ($connected) {
      loadGeo();
      loadNode();
    }
  });

  onDestroy(() => {
    clearInterval(clock);
    clearInterval(latTimer);
    document.removeEventListener("visibilitychange", onVisibility);
  });

  // Замер задержки — настоящий HTTP-запрос ядра наружу: он попадает в журнал и
  // тратит трафик. Поэтому опрос идёт только при живом соединении И открытом
  // окне. На телефоне это не мелочь: туннель остаётся поднятым в фоне часами, и
  // фоновый опрос раз в 10 секунд молча жёг бы батарею и мобильный трафик.
  function pollWanted(): boolean {
    return $connected && document.visibilityState === "visible";
  }

  function onVisibility() {
    if (pollWanted()) startLatency();
    else stopLatency();
  }

  function startLatency() {
    clearInterval(latTimer);
    measure();
    latTimer = setInterval(measure, 10000);
  }

  function stopLatency() {
    clearInterval(latTimer);
  }

  async function measure() {
    if (!pollWanted() || !currentNode) return;
    try {
      latency = await TestDelay(currentNode);
    } catch (e) {
      latency = 0;
    }
  }

  // Имя ноды нужно для замера: Clash API меряет конкретный outbound, а не
  // «текущий». API поднимается с задержкой после старта ядра — отсюда повторы.
  async function loadNode(attempt = 0) {
    try {
      currentNode = (await GetProxies()).now;
      measure();
    } catch (e) {
      if (attempt < 5 && $connected) setTimeout(() => loadNode(attempt + 1), 700);
    }
  }

  $: if ($connected) {
    if (pollWanted()) startLatency();
  } else {
    stopLatency();
    latency = 0;
  }

  // Сразу после подключения нода (Reality-хендшейк) может быть ещё не готова —
  // первые запросы падают, поэтому с повторами.
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
        // Аргумент здесь ради общей поверхности $api: на Android перехват всегда
        // полный — системного прокси нет, TUN выдаёт VpnService.
        await Connect(true);
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
  $: btnState = busy && !$connected ? "starting" : $coreState;
</script>

<svelte:window bind:innerHeight={viewportH} />

<div class="screen">
  <!-- Свечение шире экрана намеренно, поэтому лежит в отдельном слое с обрезкой:
       без неё оно вылезает за правый край и Chromium добавляет странице полосу
       горизонтальной прокрутки. -->
  <div class="bg">
    <div class="halo" class:live={$connected} />
  </div>

  <!-- Содержимое отдельным слоем: свечение позиционировано абсолютно, а
       позиционированный элемент рисуется поверх обычного потока — без обёртки
       полупрозрачный круг лёг бы на флаг и таймер. -->
  <div class="stack">
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
          <button class="icon-btn" aria-label={$t("common.refresh")} on:click={() => loadGeo()}>
            <Icon name="refresh" size={12} />
          </button>
        {/if}
      </div>
    </div>

    <!-- Таймер и задержка одной строкой: на телефоне это единственные два числа,
         которые нужны постоянно, а карточек показателей здесь нет. -->
    <div class="meter tnum" class:idle={!$since}>
      <span>{fmtDuration(elapsed)}</span>
      {#if $connected}
        <span class="sep">·</span>
        <span class="lat">{latency ? latency + " " + $t("m.ms") : "—"}</span>
      {/if}
    </div>

    <PowerButton
      state={btnState}
      size={buttonSize}
      disabled={busy || !hasProfile}
      label={$connected ? $t("connect.stop") : $t("connect.start")}
      on:click={toggle}
    />

    <div class="status">
      {#if !hasProfile}
        <span class="bad">{$t("connect.noProfile")}</span>
      {:else}
        <span class="dot" />
        <span>{$t("state." + $coreState)}</span>
      {/if}
    </div>

    {#if $errorText}<div class="error">{$errorText}</div>{/if}

    <div class="segmented modes">
      {#each MODES as m}
        <button class:on={mode === m} on:click={() => changeMode(m)}>{$t("mode." + m)}</button>
      {/each}
    </div>

    <button class="server" on:click={() => (pickerOpen = true)} disabled={!hasProfile}>
      <span class="lbl">{$t("m.server")}</span>
      <span class="val">{currentNode || "—"}</span>
      <Icon name="chevronRight" size={16} />
    </button>
  </div>
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
  .screen {
    position: relative;
    min-height: 100%;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--s-4) var(--s-4) var(--s-5);
  }

  .bg {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
  }

  .halo {
    position: absolute;
    left: 50%;
    top: 34%;
    width: 420px;
    height: 420px;
    margin: -210px 0 0 -210px;
    border-radius: 50%;
    background: radial-gradient(circle, rgba(111, 104, 144, 0.14), transparent 62%);
    pointer-events: none;
    transition: background 0.6s;
  }
  .halo.live {
    background: radial-gradient(circle, rgba(139, 92, 246, 0.24), transparent 62%);
  }

  .stack {
    position: relative;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--s-4);
  }

  .geo {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--s-2);
    text-align: center;
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
    width: 24px;
    height: 24px;
  }

  .meter {
    display: flex;
    align-items: baseline;
    gap: 10px;
    font-size: 32px;
    font-weight: 300;
    line-height: 1.1;
    letter-spacing: 0.02em;
    color: var(--text);
    transition: color 0.3s;
  }
  .meter.idle {
    color: var(--line-2);
  }
  .meter .sep {
    color: var(--muted);
  }
  .meter .lat {
    font-size: 19px;
    color: var(--text-2);
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

  .modes button {
    padding: 8px 16px;
    font-size: 13px;
  }

  /* Строка выбора сервера во всю ширину — на телефоне это основная цель после
     самой кнопки, и попадать по ней пальцем должно быть легко. */
  .server {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    width: 100%;
    max-width: 420px;
    box-sizing: border-box;
    padding: 13px var(--s-4);
    border: 1px solid var(--line);
    border-radius: var(--r-3);
    background: var(--panel);
    color: var(--text);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }
  .server:active:not(:disabled) {
    background: var(--panel-2);
  }
  .server:disabled {
    color: var(--muted);
  }
  .server .lbl {
    color: var(--text-2);
    flex: none;
  }
  .server .val {
    flex: 1;
    min-width: 0;
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
</style>
