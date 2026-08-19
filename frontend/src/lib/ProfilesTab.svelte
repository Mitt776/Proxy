<script lang="ts">
  // Вкладка профилей: список, добавление ссылками/подпиской/QR и контекстное
  // меню (QR, копирование конфига и исходных ссылок).
  import { onMount } from "svelte";
  import {
    ListProfiles, GetActiveProfileID, SetActiveProfile, AddManualProfile,
    AddSubscriptionProfile, RefreshProfile, DeleteProfile,
    ProfileConfigJSON, ProfileRaw, ProfileQR, ImportQRImage,
  } from "../../wailsjs/go/main/App";
  import { EventsOn, ClipboardSetText, ClipboardGetText } from "../../wailsjs/runtime/runtime";
  import { connected, reportError, showToast, fmtDate } from "./store";
  import type { profile } from "../../wailsjs/go/models";

  let profiles: profile.Profile[] = [];
  let activeId = "";

  let addMode: "manual" | "sub" = "manual";
  let fRaw = "";
  let fURL = "";
  let addErr = "";
  let adding = false;

  let menu = { show: false, x: 0, y: 0, id: "" };
  let qrImg = "";

  onMount(async () => {
    await load();
    EventsOn("profiles:changed", load);
  });

  async function load() {
    try {
      profiles = await ListProfiles();
      activeId = await GetActiveProfileID();
    } catch (e) {
      reportError(e);
    }
  }

  async function add() {
    addErr = "";
    adding = true;
    try {
      if (addMode === "manual") {
        if (!fRaw.trim()) throw new Error("Вставь ссылки или JSON");
        await AddManualProfile("", fRaw);
      } else {
        if (!fURL.trim()) throw new Error("Укажи URL подписки");
        await AddSubscriptionProfile("", fURL);
      }
      fRaw = "";
      fURL = "";
      await load();
    } catch (e) {
      addErr = String(e);
    } finally {
      adding = false;
    }
  }

  async function activate(id: string) {
    try {
      await SetActiveProfile(id);
      activeId = id;
    } catch (e) {
      reportError(e);
    }
  }
  async function refresh(id: string) {
    try {
      await RefreshProfile(id);
      await load();
    } catch (e) {
      reportError(e);
    }
  }
  async function remove(id: string) {
    try {
      await DeleteProfile(id);
      await load();
    } catch (e) {
      reportError(e);
    }
  }

  async function pasteClip() {
    try {
      const t = (await ClipboardGetText())?.trim();
      if (!t) return;
      if (addMode === "manual") fRaw = t;
      else fURL = t;
    } catch (e) {
      addErr = String(e);
    }
  }

  async function importQR() {
    addErr = "";
    try {
      const p = await ImportQRImage();
      if (p) await load();
    } catch (e) {
      addErr = String(e);
    }
  }

  function openMenu(e: MouseEvent, id: string) {
    e.preventDefault();
    menu = { show: true, x: e.clientX, y: e.clientY, id };
  }
  function closeMenu() {
    menu = { ...menu, show: false };
  }

  async function showQR() {
    const id = menu.id;
    closeMenu();
    try {
      qrImg = await ProfileQR(id);
    } catch (e) {
      reportError(e);
    }
  }
  async function copyJSON() {
    const id = menu.id;
    closeMenu();
    try {
      await ClipboardSetText(await ProfileConfigJSON(id));
      showToast("JSON-конфиг скопирован");
    } catch (e) {
      reportError(e);
    }
  }
  async function copyRaw() {
    const id = menu.id;
    closeMenu();
    try {
      await ClipboardSetText(await ProfileRaw(id));
      showToast("Исходные ссылки скопированы");
    } catch (e) {
      reportError(e);
    }
  }
</script>

<div class="wrap">
  <div class="plist">
    {#each profiles as p (p.id)}
      <div class="pcard" class:active={p.id === activeId}
           on:contextmenu={(e) => openMenu(e, p.id)}>
        <label class="pmain">
          <input type="radio" name="active" checked={p.id === activeId}
                 on:change={() => activate(p.id)} disabled={$connected} />
          <div class="pinfo">
            <div class="pname">{p.name}</div>
            <div class="pmeta">
              {p.kind === "subscription" ? "подписка" : "ручной"} · {p.nodeCount} нод · {fmtDate(p.updatedAt)}
            </div>
          </div>
        </label>
        <div class="pacts">
          {#if p.kind === "subscription"}
            <button class="mini" title="Обновить" on:click={() => refresh(p.id)} disabled={$connected}>⟳</button>
          {/if}
          <button class="mini danger" title="Удалить" on:click={() => remove(p.id)} disabled={$connected}>✕</button>
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
    {#if addMode === "manual"}
      <textarea class="fld" rows="4" spellcheck="false"
        placeholder="vless://…  vmess://…  hysteria2://…  (по одной в строке) или JSON-конфиг"
        bind:value={fRaw}></textarea>
    {:else}
      <input class="fld" placeholder="https://…/subscription" bind:value={fURL} />
    {/if}
    <div class="add-tools">
      <button class="mini wide" on:click={pasteClip} title="Вставить из буфера обмена">📥 Вставить</button>
      <button class="mini wide" on:click={importQR} title="Импорт ноды из картинки с QR-кодом">▦ Импорт из QR</button>
    </div>
    <div class="hint">Название сформируется автоматически из ссылки. Правый клик по профилю — QR / копировать конфиг.</div>
    {#if addErr}<div class="error">{addErr}</div>{/if}
    <button class="btn add-btn" on:click={add} disabled={adding}>
      {adding ? "Добавляю…" : "Добавить профиль"}
    </button>
  </div>
</div>

{#if menu.show}
  <div class="ctx" style="left:{menu.x}px; top:{menu.y}px;">
    <button on:click={showQR}>▦ Показать QR-код</button>
    <button on:click={copyJSON}>📋 Скопировать JSON-конфиг</button>
    <button on:click={copyRaw}>🔗 Скопировать исходные ссылки</button>
  </div>
{/if}

{#if qrImg}
  <div class="qr-overlay" role="button" tabindex="0"
       on:click={() => (qrImg = "")} on:keydown={(e) => e.key === "Escape" && (qrImg = "")}>
    <div class="qr-box" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="qr-title">Отсканируй в приложении на телефоне</div>
      <img src={qrImg} alt="QR-код профиля" />
      <button class="mini wide" on:click={() => (qrImg = "")}>Закрыть</button>
    </div>
  </div>
{/if}

<svelte:window on:click={closeMenu} on:keydown={(e) => e.key === "Escape" && closeMenu()} />

<style>
  .wrap { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .plist { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; min-height: 60px; }
  .pcard {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    background: var(--bg); border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px;
  }
  .pcard.active { border-color: var(--accent); background: var(--accent-bg); }
  .pmain { display: flex; align-items: center; gap: 10px; cursor: pointer; flex: 1; min-width: 0; }
  .pinfo { min-width: 0; text-align: left; }
  .pname { font-weight: 700; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pmeta { font-size: 11px; color: var(--muted); }
  .pacts { display: flex; gap: 4px; flex: none; }

  .add {
    margin-top: 12px; border-top: 1px solid var(--line); padding-top: 12px;
    display: flex; flex-direction: column; gap: 8px;
  }
  .tabs { display: flex; gap: 6px; }
  .tabs button {
    flex: 1; background: var(--bg); border: 1px solid var(--line); color: var(--text-2);
    padding: 6px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit;
  }
  .tabs button.on { background: #1f6feb; border-color: #1f6feb; color: #fff; font-weight: 700; }
  .add-tools { display: flex; gap: 6px; }
  .add-tools .mini.wide { flex: 1; }

  .ctx {
    position: fixed; z-index: 100; background: #1c2128; border: 1px solid var(--line-2);
    border-radius: 8px; padding: 4px; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
    display: flex; flex-direction: column; min-width: 210px;
  }
  .ctx button {
    background: none; border: none; color: var(--text); text-align: left;
    padding: 8px 10px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit;
  }
  .ctx button:hover { background: var(--line); }

  .qr-overlay {
    position: fixed; inset: 0; z-index: 200; background: rgba(0, 0, 0, 0.65);
    display: flex; align-items: center; justify-content: center;
  }
  .qr-box {
    background: var(--panel); border: 1px solid var(--line-2); border-radius: 12px; padding: 18px;
    display: flex; flex-direction: column; align-items: center; gap: 12px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.6);
  }
  .qr-title { font-size: 13px; color: var(--text-2); font-weight: 700; }
  .qr-box img { width: 260px; height: 260px; background: #fff; border-radius: 8px; padding: 8px; image-rendering: pixelated; }
</style>
