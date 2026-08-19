<script lang="ts">
  // Форма одного правила: что ловим, что с этим делаем и куда шлём.
  import { createEventDispatcher } from "svelte";
  import ProcessPicker from "./ProcessPicker.svelte";
  import type { rules } from "../../wailsjs/go/models";

  export let rule: rules.Rule;
  export let groups: rules.Group[] = [];
  // Удалённые наборы правил из routing.json — показываем рядом со встроенными.
  export let ruleSets: rules.RuleSet[] = [];

  const dispatch = createEventDispatcher<{ save: rules.Rule; cancel: void }>();

  // Локальная копия: пока пользователь не нажал «Сохранить», список правил
  // трогать нельзя — иначе ядро перезапустится на полуготовом правиле.
  let draft: rules.Rule = { ...rule, values: [...(rule.values || [])] };
  let valuesText = (draft.values || []).join("\n");
  let showPicker = false;

  const matches = [
    { id: "domainSuffix", label: "Домен и поддомены", ph: "youtube.com" },
    { id: "domain", label: "Домен точно", ph: "example.com" },
    { id: "domainKeyword", label: "Часть домена", ph: "tracker" },
    { id: "domainRegex", label: "Домен по регулярке", ph: "^ads\\..*\\.com$" },
    { id: "ipCIDR", label: "IP или подсеть", ph: "8.8.8.8\n10.0.0.0/8" },
    { id: "port", label: "Порт", ph: "25\n465" },
    { id: "process", label: "Процесс", ph: "qbittorrent.exe" },
    { id: "processPath", label: "Путь к программе", ph: "C:\\Program Files\\app.exe" },
    { id: "ruleSet", label: "Готовый набор", ph: "geosite-ru" },
    { id: "private", label: "Приватные сети (LAN)", ph: "" },
    { id: "protocol", label: "Протокол", ph: "quic" },
    { id: "network", label: "Тип трафика", ph: "udp" },
  ];

  const builtinSets = [
    { id: "geosite-ru", label: "Российские сайты" },
    { id: "geoip-ru", label: "Российские IP" },
    { id: "geosite-ads", label: "Рекламные домены" },
  ];
  $: allSets = [...builtinSets, ...ruleSets.map((s) => ({ id: s.tag, label: s.tag }))];

  const protocols = ["quic", "dns", "http", "tls", "bittorrent", "stun", "ssh", "rdp", "dtls"];

  $: matchInfo = matches.find((m) => m.id === draft.match) || matches[0];
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
    if (draft.action === "block") draft.tlsFragment = false;
    if (draft.action !== "proxy") draft.target = "";
    dispatch("save", draft);
  }
</script>

<div class="overlay" role="button" tabindex="0"
     on:click={() => dispatch("cancel")}
     on:keydown={(e) => e.key === "Escape" && dispatch("cancel")}>
  <div class="box" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="title">{rule.id ? "Правило" : "Новое правило"}</div>

    <label class="row">
      <span class="lbl">Название</span>
      <input class="fld" bind:value={draft.name} placeholder="Торренты мимо прокси" />
    </label>

    <label class="row">
      <span class="lbl">Ловим</span>
      <select class="fld" bind:value={draft.match} disabled={draft.builtin}>
        {#each matches as m}<option value={m.id}>{m.label}</option>{/each}
      </select>
    </label>

    {#if needsValues}
      <div class="row">
        <span class="lbl">Значения</span>
        <div class="vals">
          {#if draft.match === "ruleSet"}
            <div class="chips">
              {#each allSets as rs}
                <button class="chip" class:on={valuesText.includes(rs.id)}
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
                      placeholder={matchInfo.ph} bind:value={valuesText}></textarea>
            <div class="hint">По одному значению в строке.</div>
          {/if}
          {#if draft.match === "process" || draft.match === "processPath"}
            <button class="mini wide" on:click={() => (showPicker = true)}>
              🖥 Выбрать из запущенных
            </button>
          {/if}
        </div>
      </div>
    {/if}

    <label class="row">
      <span class="lbl">Действие</span>
      <select class="fld" bind:value={draft.action}>
        <option value="proxy">Через прокси</option>
        <option value="direct">Напрямую</option>
        <option value="block">Заблокировать</option>
      </select>
    </label>

    {#if draft.action === "proxy" && groups.length}
      <label class="row">
        <span class="lbl">Через</span>
        <select class="fld" bind:value={draft.target}>
          <option value="">Основной выбор</option>
          {#each groups as g}<option value={g.name}>{g.name}</option>{/each}
        </select>
      </label>
    {/if}

    {#if draft.action === "block"}
      <label class="row">
        <span class="lbl">Как</span>
        <select class="fld" bind:value={draft.method}>
          <option value="">Отказать (приложение сразу видит ошибку)</option>
          <option value="drop">Молча отбросить (нужно для QUIC)</option>
        </select>
      </label>
    {:else}
      <label class="row check-row">
        <span class="lbl"></span>
        <span class="check">
          <input type="checkbox" bind:checked={draft.tlsFragment} />
          Резать TLS-приветствие (обход блокировки по имени сайта)
        </span>
      </label>
    {/if}

    <div class="foot">
      <button class="mini wide" on:click={() => dispatch("cancel")}>Отмена</button>
      <button class="btn add-btn small" on:click={save}>Сохранить</button>
    </div>
  </div>
</div>

{#if showPicker}
  <ProcessPicker selected={valuesText.split("\n").filter(Boolean)}
                 on:apply={applyProcesses} on:close={() => (showPicker = false)} />
{/if}

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 200; background: rgba(0, 0, 0, 0.65);
    display: flex; align-items: center; justify-content: center;
  }
  .box {
    background: var(--panel); border: 1px solid var(--line-2); border-radius: 12px;
    padding: 18px; width: min(560px, 92vw); max-height: 88vh; overflow-y: auto;
    display: flex; flex-direction: column; gap: 10px; text-align: left;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.6);
  }
  .title { font-size: 15px; font-weight: 800; margin-bottom: 4px; }
  .row { display: flex; align-items: flex-start; gap: 10px; }
  .lbl { width: 90px; flex: none; font-size: 13px; color: var(--muted); padding-top: 7px; }
  .row > .fld, .vals { flex: 1; min-width: 0; }
  .vals { display: flex; flex-direction: column; gap: 6px; }
  .vals textarea { width: 100%; box-sizing: border-box; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip {
    background: var(--bg); border: 1px solid var(--line); color: var(--text-2);
    border-radius: 999px; padding: 4px 12px; font-size: 12px; cursor: pointer;
    font-family: inherit;
  }
  .chip.on { background: var(--accent-bg); border-color: var(--accent); color: var(--text); font-weight: 700; }
  .check-row .check { padding-top: 5px; }
  .foot { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
  .btn.small { padding: 6px 16px; font-size: 13px; }
</style>
