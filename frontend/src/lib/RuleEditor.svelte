<script lang="ts">
  // Форма одного правила: что ловим, что с этим делаем и куда шлём.
  import { createEventDispatcher } from "svelte";
  import Icon from "./icons/Icon.svelte";
  import { t } from "./i18n";
  import type { rules } from "../../wailsjs/go/models";

  export let rule: rules.Rule;
  export let groups: rules.Group[] = [];
  // Удалённые наборы правил из routing.json — показываем рядом со встроенными.
  export let ruleSets: rules.RuleSet[] = [];

  /**
   * Пикер процессов — компонентом снаружи, а не импортом здесь.
   *
   * Он весь на WinAPI (`ListProcesses`), которого в мобильном мосте нет вовсе, и
   * обычный import утащил бы его в сборку APK — та не собралась бы на пропавшем
   * экспорте. Заодно один этот проп решает и вторую половину дела: без него
   * пропадают матчеры по процессу, которых на Android всё равно нет.
   */
  export let processPicker: any = null;

  const dispatch = createEventDispatcher<{ save: rules.Rule; cancel: void }>();

  // Локальная копия: пока пользователь не нажал «Сохранить», список правил
  // трогать нельзя — иначе ядро перезапустится на полуготовом правиле.
  let draft: rules.Rule = { ...rule, values: [...(rule.values || [])] };
  let valuesText = (draft.values || []).join("\n");
  let showPicker = false;

  // Подсказки-примеры языка не имеют — они одинаковы в обоих словарях.
  const placeholders: Record<string, string> = {
    domainSuffix: "youtube.com",
    domain: "example.com",
    domainKeyword: "tracker",
    domainRegex: "^ads\\..*\\.com$",
    ipCIDR: "8.8.8.8\n10.0.0.0/8",
    port: "25\n465",
    process: "qbittorrent.exe",
    processPath: "C:\\Program Files\\app.exe",
    ruleSet: "geosite-ru",
    private: "",
    protocol: "quic",
    network: "udp",
  };
  const allMatches = Object.keys(placeholders);
  const processMatches = ["process", "processPath"];

  // Уже выбранный матчер оставляем в списке всегда: правило могло приехать из
  // routing.json, написанного на другой платформе, и пустой select в форме
  // выглядел бы как потерянная настройка.
  $: matches = allMatches.filter(
    (m) => processPicker || !processMatches.includes(m) || draft.match === m,
  );

  // Встроенные наборы лежат в assets готовыми .srs — их подписи переводятся,
  // добавленные пользователем показываются своим тегом.
  const builtinSets = [
    { id: "geosite-ru", key: "editor.set.geositeRu" },
    { id: "geoip-ru", key: "editor.set.geoipRu" },
    { id: "geosite-ads", key: "editor.set.geositeAds" },
  ];

  const protocols = ["quic", "dns", "http", "tls", "bittorrent", "stun", "ssh", "rdp", "dtls"];

  $: allSets = [
    ...builtinSets.map((s) => ({ id: s.id, label: $t(s.key) })),
    ...ruleSets.map((s) => ({ id: s.tag, label: s.tag })),
  ];
  $: needsValues = draft.match !== "private";

  function applyProcesses(e: CustomEvent<string[]>) {
    const names = e.detail;
    const have = valuesText.split("\n").map((s) => s.trim()).filter(Boolean);
    valuesText = Array.from(new Set([...have, ...names])).join("\n");
    showPicker = false;
  }

  function toggleValue(v: string) {
    const have = valuesText.split("\n").map((s) => s.trim()).filter(Boolean);
    const next = have.includes(v) ? have.filter((x) => x !== v) : [...have, v];
    valuesText = next.join("\n");
  }

  function save() {
    draft.values = valuesText.split("\n").map((s) => s.trim()).filter(Boolean);
    if (draft.action !== "block") draft.method = "";
    // Фрагментация имеет смысл только на прямом соединении: у прокси-правила
    // разрез остаётся внутри туннеля и до провайдера не доезжает, а бэкенд
    // такое правило отвергнет.
    if (draft.action !== "direct") draft.tlsFragment = false;
    if (draft.action !== "proxy") draft.target = "";
    dispatch("save", draft);
  }
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("cancel")}
     on:keydown={(e) => e.key === "Escape" && dispatch("cancel")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {rule.id ? $t("editor.title") : $t("editor.titleNew")}
      <button class="icon-btn" on:click={() => dispatch("cancel")} title={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="modal-b">
      <label class="row">
        <span class="lbl">{$t("editor.name")}</span>
        <input class="fld" bind:value={draft.name} placeholder={$t("editor.namePh")} />
      </label>

      <label class="row">
        <span class="lbl">{$t("editor.match")}</span>
        <select class="fld" bind:value={draft.match} disabled={draft.builtin}>
          {#each matches as m}<option value={m}>{$t("editor.m." + m)}</option>{/each}
        </select>
      </label>

      {#if needsValues}
        <div class="row">
          <span class="lbl">{$t("editor.values")}</span>
          <div class="vals">
            {#if draft.match === "ruleSet"}
              <div class="chips">
                {#each allSets as rs}
                  <button class="chip" class:on={valuesText.split("\n").includes(rs.id)}
                          on:click={() => toggleValue(rs.id)}>{rs.label}</button>
                {/each}
              </div>
            {:else if draft.match === "protocol"}
              <div class="chips">
                {#each protocols as p}
                  <button class="chip" class:on={valuesText.split("\n").includes(p)}
                          on:click={() => toggleValue(p)}>{p}</button>
                {/each}
              </div>
            {:else if draft.match === "network"}
              <div class="chips">
                {#each ["tcp", "udp"] as n}
                  <button class="chip" class:on={valuesText.split("\n").includes(n)}
                          on:click={() => toggleValue(n)}>{n}</button>
                {/each}
              </div>
            {:else}
              <textarea class="fld" rows="4" spellcheck="false"
                        placeholder={placeholders[draft.match] || ""} bind:value={valuesText}></textarea>
              <div class="hint">{$t("editor.valuesHint")}</div>
            {/if}
            {#if processPicker && (draft.match === "process" || draft.match === "processPath")}
              <button class="btn sm" on:click={() => (showPicker = true)}>
                <Icon name="folder" size={13} />{$t("editor.pickProcess")}
              </button>
            {/if}
          </div>
        </div>
      {/if}

      <label class="row">
        <span class="lbl">{$t("editor.action")}</span>
        <select class="fld" bind:value={draft.action}>
          <option value="proxy">{$t("editor.action.proxy")}</option>
          <option value="direct">{$t("editor.action.direct")}</option>
          <option value="block">{$t("editor.action.block")}</option>
        </select>
      </label>

      {#if draft.action === "proxy" && groups.length}
        <label class="row">
          <span class="lbl">{$t("editor.via")}</span>
          <select class="fld" bind:value={draft.target}>
            <option value="">{$t("editor.viaDefault")}</option>
            {#each groups as g}<option value={g.name}>{g.name}</option>{/each}
          </select>
        </label>
      {/if}

      {#if draft.action === "block"}
        <label class="row">
          <span class="lbl">{$t("editor.how")}</span>
          <select class="fld" bind:value={draft.method}>
            <option value="">{$t("editor.block.reject")}</option>
            <option value="drop">{$t("editor.block.drop")}</option>
          </select>
        </label>
      {:else if draft.action === "direct"}
        <div class="row">
          <span class="lbl"></span>
          <label class="check frag">
            <input type="checkbox" bind:checked={draft.tlsFragment} />
            <span>
              {$t("editor.frag")}
              <span class="note">{$t("editor.fragNote")}</span>
            </span>
          </label>
        </div>
      {/if}
    </div>

    <div class="modal-f">
      <button class="btn" on:click={() => dispatch("cancel")}>{$t("common.cancel")}</button>
      <button class="btn primary" on:click={save}>{$t("common.save")}</button>
    </div>
  </div>
</div>

{#if showPicker && processPicker}
  <svelte:component this={processPicker} selected={valuesText.split("\n").filter(Boolean)}
                    on:apply={applyProcesses} on:close={() => (showPicker = false)} />
{/if}

<style>
  .modal-b { display: flex; flex-direction: column; gap: var(--s-3); }

  .row { display: flex; align-items: flex-start; gap: var(--s-3); }
  .lbl { width: 96px; flex: none; font-size: 12.5px; color: var(--muted); padding-top: 9px; }
  .row > .fld, .vals { flex: 1; min-width: 0; }
  .vals { display: flex; flex-direction: column; gap: var(--s-2); align-items: flex-start; }
  .vals textarea { width: 100%; box-sizing: border-box; }

  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip {
    background: var(--bg);
    border: 1px solid var(--line);
    color: var(--text-2);
    border-radius: var(--r-pill);
    padding: 5px 13px;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, color 0.15s;
  }
  .chip:hover:not(.on) { border-color: var(--line-2); color: var(--text); }
  .chip.on {
    background: var(--accent-dim);
    border-color: var(--accent);
    color: var(--accent-2);
    font-weight: 700;
  }

  .frag { padding-top: 7px; align-items: flex-start; line-height: 1.45; }
  .frag .note { display: block; margin-top: 3px; font-size: 11.5px; color: var(--muted); }

  /* На телефоне колонка подписей съедает четверть ширины, и в поле значений не
     помещается даже домен средней длины — там форма складывается в столбик. */
  @media (max-width: 560px) {
    .row { flex-direction: column; gap: 4px; }
    .lbl { width: auto; padding-top: 0; }
    /* box-sizing обязателен: у .fld свои 12 px отступа с каждой стороны, и без
       него поле на 100% ширины вылезает за модалку вместе с полосой прокрутки. */
    .row > .fld, .vals { width: 100%; box-sizing: border-box; }
  }
</style>
