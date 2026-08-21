<script lang="ts">
  // Профили на телефоне: список строк вместо сетки карточек и три способа
  // импорта, которых на Windows нет, — из буфера, камерой и из галереи.
  //
  // Вся работа с профилями общая (lib/profiles.ts), различается только вид и
  // способ принести ссылку: сетка карточек с правым кликом на экран 360 px не
  // ложится, а файлового диалога на Android не существует.
  import { onMount } from "svelte";
  import { EventsOn, PickQRImage, ScanQR } from "$api";
  import {
    activateProfile, activeProfileID, addProfile, clipboardText, copyProfileRaw,
    deleteProfile, loadProfiles, profileQR, profiles, refreshProfile,
  } from "../profiles";
  import { connected, fmtDate, reportError, showToast } from "../store";
  import { t, tp, tr } from "../i18n";
  import { errText } from "../i18n/errors";
  import Icon from "../icons/Icon.svelte";

  let showAdd = false;
  let addMode: "manual" | "sub" = "manual";
  let fRaw = "";
  let fURL = "";
  let addErr = "";
  let adding = false;

  let qrImg = "";
  /** Профиль, для которого открыто меню действий; «» — меню закрыто. */
  let sheetID = "";

  onMount(async () => {
    await loadProfiles();
    EventsOn("profiles:changed", loadProfiles);
  });

  function openAdd() {
    addErr = "";
    showAdd = true;
  }

  async function add() {
    addErr = "";
    adding = true;
    try {
      await addProfile(addMode, addMode === "manual" ? fRaw : fURL);
      fRaw = "";
      fURL = "";
      showAdd = false;
    } catch (e) {
      addErr = errText(e);
    } finally {
      adding = false;
    }
  }

  async function pasteClip() {
    try {
      const s = await clipboardText();
      if (!s) return;
      if (addMode === "manual") fRaw = s;
      else fURL = s;
    } catch (e) {
      addErr = errText(e);
    }
  }

  // Камера и галерея делают всё сами: системный экран возвращает картинку,
  // Go распознаёт QR и заводит профиль. Отмена приходит пустым именем — это не
  // ошибка, показывать её незачем.
  async function importFrom(pick: () => Promise<string>) {
    try {
      const name = await pick();
      if (!name) return;
      showAdd = false;
      await loadProfiles();
      showToast(tr("m.profiles.imported", { name }));
    } catch (e) {
      addErr = errText(e);
      reportError(e);
    }
  }

  async function showQR(id: string) {
    sheetID = "";
    qrImg = await profileQR(id);
  }

  $: sheetProfile = $profiles.find((p) => p.id === sheetID) ?? null;
</script>

<div class="wrap">
  <header>
    <h1>{$t("tab.profiles")}</h1>
    <button class="btn primary sm" on:click={openAdd}>
      <Icon name="plus" size={15} />{$t("profiles.add")}
    </button>
  </header>

  {#if $profiles.length === 0}
    <div class="none">
      <Icon name="layers" size={30} />
      <div class="none-t">{$t("profiles.empty")}</div>
      <div class="hint">{$t("profiles.emptyHint")}</div>
    </div>
  {:else}
    <ul class="list">
      {#each $profiles as p (p.id)}
        <li class="row" class:on={p.id === $activeProfileID}>
          <!-- Нажатие по строке активирует профиль: это то, чего от списка ждут.
               Остальное — под кнопкой с многоточием, чтобы не ловить пальцем
               четыре иконки в ряд. -->
          <button class="pick" on:click={() => activateProfile(p.id)}>
            <span class="dot" class:idle={p.id !== $activeProfileID} />
            <span class="txt">
              <span class="pname">{p.name}</span>
              <span class="pmeta">
                {$tp("profiles.nodes", p.nodeCount)} · {$fmtDate(p.updatedAt)}
              </span>
            </span>
            {#if p.kind === "subscription"}
              <span class="pill accent">{$t("profiles.kind.subscription")}</span>
            {/if}
          </button>
          <button class="more" aria-label={$t("common.edit")} on:click={() => (sheetID = p.id)}>
            <Icon name="drag" size={18} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<!-- Меню действий над профилем: снизу, как принято на телефоне. -->
{#if sheetProfile}
  <div class="modal-backdrop sheet-back" role="button" tabindex="0"
       on:click={() => (sheetID = "")}
       on:keydown={(e) => e.key === "Escape" && (sheetID = "")}>
    <div class="sheet" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="sheet-h">{sheetProfile.name}</div>
      <button class="act" on:click={() => showQR(sheetProfile.id)}>
        <Icon name="qr" size={17} />{$t("profiles.showQR")}
      </button>
      <button class="act" on:click={() => { const id = sheetProfile.id; sheetID = ""; copyProfileRaw(id); }}>
        <Icon name="link" size={17} />{$t("profiles.copyRaw")}
      </button>
      {#if sheetProfile.kind === "subscription"}
        <button class="act" disabled={$connected}
                on:click={() => { const id = sheetProfile.id; sheetID = ""; refreshProfile(id); }}>
          <Icon name="refresh" size={17} />{$t("common.refresh")}
        </button>
      {/if}
      <button class="act danger" disabled={$connected}
              on:click={() => { const id = sheetProfile.id; sheetID = ""; deleteProfile(id); }}>
        <Icon name="trash" size={17} />{$t("common.delete")}
      </button>
    </div>
  </div>
{/if}

{#if showAdd}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (showAdd = false)}
       on:keydown={(e) => e.key === "Escape" && (showAdd = false)}>
    <div class="modal add" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("profiles.addTitle")}
        <button class="icon-btn" on:click={() => (showAdd = false)} aria-label={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>

      <div class="modal-b">
        <!-- Импорт вынесен наверх: на телефоне ссылку почти всегда приносят
             извне, а руками её здесь не наберёт никто. -->
        <div class="imports">
          <button class="btn" on:click={() => importFrom(ScanQR)}>
            <Icon name="qr" size={15} />{$t("m.profiles.camera")}
          </button>
          <button class="btn" on:click={() => importFrom(PickQRImage)}>
            <Icon name="folder" size={15} />{$t("m.profiles.gallery")}
          </button>
          <button class="btn" on:click={pasteClip}>
            <Icon name="copy" size={15} />{$t("profiles.paste")}
          </button>
        </div>

        <div class="segmented wide">
          <button class:on={addMode === "manual"} on:click={() => (addMode = "manual")}>
            {$t("profiles.manual")}
          </button>
          <button class:on={addMode === "sub"} on:click={() => (addMode = "sub")}>
            {$t("profiles.sub")}
          </button>
        </div>

        {#if addMode === "manual"}
          <textarea class="fld" spellcheck="false" rows="4"
            placeholder={$t("profiles.rawPh")} bind:value={fRaw}></textarea>
        {:else}
          <input class="fld" placeholder={$t("profiles.urlPh")} bind:value={fURL} />
        {/if}

        <div class="hint">{$t("profiles.autoName")}</div>
        {#if addErr}<div class="error">{addErr}</div>{/if}
      </div>

      <div class="modal-f">
        <button class="btn" on:click={() => (showAdd = false)}>{$t("common.cancel")}</button>
        <button class="btn primary" on:click={add} disabled={adding}>
          {adding ? $t("profiles.adding") : $t("common.add")}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if qrImg}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (qrImg = "")} on:keydown={(e) => e.key === "Escape" && (qrImg = "")}>
    <div class="modal qr" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("profiles.qrTitle")}
        <button class="icon-btn" on:click={() => (qrImg = "")} aria-label={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>
      <div class="modal-b qr-b"><img src={qrImg} alt="QR" /></div>
    </div>
  </div>
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

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s-2);
  }
  h1 {
    margin: 0;
    font-size: 19px;
    font-weight: 700;
  }

  .none {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--s-2);
    padding: var(--s-6) var(--s-4);
    color: var(--muted);
    text-align: center;
  }
  .none-t {
    font-size: 15px;
    font-weight: 600;
    color: var(--text-2);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }

  .row {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--line);
    border-radius: var(--r-3);
    background: var(--panel);
    overflow: hidden;
  }
  .row.on {
    border-color: var(--accent);
    background: var(--accent-dim);
  }

  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--s-3);
    padding: 13px var(--s-3);
    border: none;
    background: transparent;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .pick .dot {
    width: 8px;
    height: 8px;
  }
  .pick .dot.idle {
    background: var(--line-2);
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .pname {
    font-size: 14.5px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pmeta {
    font-size: 11.5px;
    color: var(--muted);
  }

  .more {
    flex: none;
    width: 46px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-left: 1px solid var(--line);
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .more:active {
    background: var(--panel-2);
  }

  /* Лист действий прижат к низу экрана — там, где до него дотягивается палец. */
  .sheet-back {
    align-items: flex-end;
    padding: 0;
  }
  .sheet {
    width: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--s-2) var(--s-2) calc(var(--s-3) + var(--safe-bottom));
    background: var(--panel);
    border-top: 1px solid var(--line-2);
    border-radius: var(--r-4) var(--r-4) 0 0;
  }
  .sheet-h {
    padding: var(--s-3) var(--s-3) var(--s-2);
    font-size: 12px;
    font-weight: 700;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .act {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    padding: 14px var(--s-3);
    border: none;
    border-radius: var(--r-2);
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 15px;
    text-align: left;
    cursor: pointer;
  }
  .act:active:not(:disabled) {
    background: var(--panel-2);
  }
  .act:disabled {
    color: var(--muted);
  }
  .act.danger {
    color: var(--danger);
  }

  .imports {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--s-2);
  }
  .imports .btn {
    flex-direction: column;
    gap: 5px;
    padding: 11px 4px;
    font-size: 11.5px;
  }

  .modal-b {
    display: flex;
    flex-direction: column;
    gap: var(--s-3);
  }
  .qr-b {
    align-items: center;
  }
  .qr-b img {
    width: 100%;
    max-width: 280px;
    image-rendering: pixelated;
    border-radius: var(--r-2);
    background: #fff;
    padding: var(--s-3);
    box-sizing: border-box;
  }
</style>
