<script lang="ts">
  // Вкладка профилей: сетка карточек, добавление ссылками/подпиской/QR и
  // контекстное меню (QR, копирование конфига и исходных ссылок). Всё, что
  // нужно редко, живёт в модалках — на экране остаётся сам список.
  import { onMount } from "svelte";
  import { ImportQRImage } from "$api";
  import { EventsOn } from "$api";
  import { connected, reportError, fmtDate } from "./store";
  import {
    activateProfile, activeProfileID, addProfile, clipboardText, copyProfileJSON,
    copyProfileRaw, deleteProfile, loadProfiles, profileQR, profiles, refreshProfile,
  } from "./profiles";
  import { t, tp } from "./i18n";
  import { errText } from "./i18n/errors";
  import Icon from "./icons/Icon.svelte";
  import TabHead from "./shell/TabHead.svelte";

  let showAdd = false;
  let addMode: "manual" | "sub" = "manual";
  let fRaw = "";
  let fURL = "";
  let addErr = "";
  let adding = false;

  let menu = { show: false, x: 0, y: 0, id: "" };
  let qrImg = "";

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

  async function importQR() {
    try {
      const p = await ImportQRImage();
      if (p) await loadProfiles();
    } catch (e) {
      reportError(e);
    }
  }

  // Правый клик оставлен как второй путь к тем же трём действиям: привычка
  // никуда не делась, а на карточке иконки видны и без него.
  function openMenu(e: MouseEvent, id: string) {
    e.preventDefault();
    menu = { show: true, x: e.clientX, y: e.clientY, id };
  }
  function closeMenu() {
    menu = { ...menu, show: false };
  }

  async function showQR(id: string) {
    closeMenu();
    qrImg = await profileQR(id);
  }
  async function copyJSON(id: string) {
    closeMenu();
    await copyProfileJSON(id);
  }
  async function copyRaw(id: string) {
    closeMenu();
    await copyProfileRaw(id);
  }
</script>

<div class="tab-wrap">
  <TabHead title={$t("tab.profiles")} sub={$t("profiles.subtitle")}>
    <button class="btn primary" on:click={openAdd}>
      <Icon name="plus" size={15} />{$t("profiles.add")}
    </button>
  </TabHead>

  {#if $profiles.length === 0}
    <div class="panel none">
      <Icon name="layers" size={30} />
      <div class="none-t">{$t("profiles.empty")}</div>
      <div class="hint">{$t("profiles.emptyHint")}</div>
      <button class="btn primary" on:click={openAdd}>
        <Icon name="plus" size={15} />{$t("profiles.add")}
      </button>
    </div>
  {:else}
    <div class="grid">
      {#each $profiles as p (p.id)}
        <div class="pcard card" class:on={p.id === $activeProfileID}
             on:contextmenu={(e) => openMenu(e, p.id)}>
          <div class="top">
            <span class="pname" title={p.name}>{p.name}</span>
            <span class="pill" class:accent={p.kind === "subscription"}>
              {p.kind === "subscription" ? $t("profiles.kind.subscription") : $t("profiles.kind.manual")}
            </span>
          </div>
          <div class="pmeta">
            {$tp("profiles.nodes", p.nodeCount)} · {$fmtDate(p.updatedAt)}
          </div>
          <div class="pacts">
            {#if p.id === $activeProfileID}
              <span class="pill accent"><Icon name="check" size={11} />{$t("profiles.active")}</span>
            {:else}
              <!-- Не блокируем на живом соединении: SetActiveProfile пересобирает
                   конфиг и перезапускает ядро, не снимая системный прокси. -->
              <button class="btn sm" on:click={() => activateProfile(p.id)}>
                {$t("profiles.activate")}
              </button>
            {/if}
            <span class="sp"></span>
            <button class="icon-btn" title={$t("profiles.showQR")} on:click={() => showQR(p.id)}>
              <Icon name="qr" size={14} />
            </button>
            <button class="icon-btn" title={$t("profiles.copyJSON")} on:click={() => copyJSON(p.id)}>
              <Icon name="copy" size={14} />
            </button>
            {#if p.kind === "subscription"}
              <button class="icon-btn" title={$t("common.refresh")} disabled={$connected}
                      on:click={() => refreshProfile(p.id)}>
                <Icon name="refresh" size={14} />
              </button>
            {/if}
            <button class="icon-btn danger" title={$t("common.delete")} disabled={$connected}
                    on:click={() => deleteProfile(p.id)}>
              <Icon name="trash" size={14} />
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showAdd}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (showAdd = false)}
       on:keydown={(e) => e.key === "Escape" && (showAdd = false)}>
    <div class="modal add" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("profiles.addTitle")}
        <button class="icon-btn" on:click={() => (showAdd = false)} title={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>

      <div class="modal-b">
        <div class="segmented wide">
          <button class:on={addMode === "manual"} on:click={() => (addMode = "manual")}>
            {$t("profiles.manual")}
          </button>
          <button class:on={addMode === "sub"} on:click={() => (addMode = "sub")}>
            {$t("profiles.sub")}
          </button>
        </div>

        <!-- Обёртка фиксированной высоты: у ссылок многострочное поле, у подписки
             однострочное, и без неё модалка прыгала бы при переключении. -->
        <div class="fbox">
          {#if addMode === "manual"}
            <textarea class="fld" spellcheck="false"
              placeholder={$t("profiles.rawPh")} bind:value={fRaw}></textarea>
          {:else}
            <input class="fld" placeholder={$t("profiles.urlPh")} bind:value={fURL} />
          {/if}
        </div>

        <div class="tools">
          <button class="btn sm" on:click={pasteClip}>
            <Icon name="copy" size={13} />{$t("profiles.paste")}
          </button>
          <button class="btn sm" on:click={importQR}>
            <Icon name="qr" size={13} />{$t("profiles.importQR")}
          </button>
        </div>

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

{#if menu.show}
  <div class="ctx" style="left:{menu.x}px; top:{menu.y}px;">
    <button on:click={() => showQR(menu.id)}>
      <Icon name="qr" size={14} />{$t("profiles.showQR")}
    </button>
    <button on:click={() => copyJSON(menu.id)}>
      <Icon name="copy" size={14} />{$t("profiles.copyJSON")}
    </button>
    <button on:click={() => copyRaw(menu.id)}>
      <Icon name="link" size={14} />{$t("profiles.copyRaw")}
    </button>
  </div>
{/if}

{#if qrImg}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (qrImg = "")} on:keydown={(e) => e.key === "Escape" && (qrImg = "")}>
    <div class="modal qr" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("profiles.qrTitle")}
        <button class="icon-btn" on:click={() => (qrImg = "")} title={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>
      <div class="modal-b qr-b">
        <img src={qrImg} alt={$t("profiles.qrTitle")} />
        <div class="hint">{$t("profiles.qrHint")}</div>
      </div>
    </div>
  </div>
{/if}

<svelte:window on:click={closeMenu} on:keydown={(e) => e.key === "Escape" && closeMenu()} />

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(268px, 1fr));
    gap: var(--s-3);
    align-content: start;
    overflow-y: auto;
    min-height: 0;
    padding-bottom: var(--s-3);
  }

  .pcard {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 13px 14px;
    transition: border-color 0.15s, background 0.15s;
  }
  /* Активный профиль — акцентная рамка и подсветка: он один, и глаз должен
     находить его в сетке без чтения бейджей. */
  .pcard.on {
    border-color: var(--accent);
    background: linear-gradient(180deg, var(--accent-dim), transparent 70%), var(--panel-2);
  }

  .top { display: flex; align-items: center; gap: var(--s-2); min-width: 0; }
  .pname {
    flex: 1;
    min-width: 0;
    font-size: 14px;
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pmeta { font-size: 11.5px; color: var(--muted); }

  .pacts { display: flex; align-items: center; gap: 4px; margin-top: 3px; }
  .sp { flex: 1; }

  /* Пустой экран: одна крупная подсказка вместо списка. */
  .none {
    align-items: center;
    justify-content: center;
    gap: var(--s-3);
    padding: var(--s-6);
    color: var(--muted);
    flex: 1;
  }
  .none-t { font-size: 15px; font-weight: 600; color: var(--text-2); }

  .modal.add { max-width: 520px; }
  .modal-b { display: flex; flex-direction: column; gap: var(--s-3); }

  .fbox { height: 148px; display: flex; }
  .fbox .fld { width: 100%; box-sizing: border-box; }
  .fbox textarea { resize: none; }
  .fbox input { align-self: flex-start; }
  .segmented.wide { display: flex; }
  .segmented.wide button { flex: 1; }
  .modal-b .tools { display: flex; gap: var(--s-2); }
  .modal-b .tools .btn { flex: 1; }

  .modal.qr { max-width: 340px; }
  .qr-b { align-items: center; gap: var(--s-3); }
  .qr-b img {
    width: 260px;
    height: 260px;
    background: #fff;
    border-radius: var(--r-2);
    padding: 10px;
    box-sizing: border-box;
    image-rendering: pixelated;
  }

  .ctx {
    position: fixed;
    z-index: 320;
    background: var(--panel-2);
    border: 1px solid var(--line-2);
    border-radius: var(--r-2);
    padding: 4px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    min-width: 220px;
  }
  .ctx button {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    background: none;
    border: none;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--r-1);
    cursor: pointer;
    font: inherit;
    font-size: 13px;
  }
  .ctx button:hover { background: var(--line); }
</style>
