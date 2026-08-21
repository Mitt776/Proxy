<script lang="ts">
  // Маршрутизация на телефоне. Отдельный компонент, а не ветки в общей вкладке:
  // на десктопе приоритет правил меняют перетаскиванием, а HTML5 drag&drop на
  // тач-экране не работает вовсе — порядок здесь двигают кнопками из меню
  // действий. Плюс строка правила на 360 px не вмещает шесть элементов подряд,
  // а шапка — четыре кнопки.
  //
  // Форма правила (RuleEditor) общая с Windows: она и так столбик, а матчеры по
  // процессу пропадают сами — им нужен пикер, которого мы сюда не передаём.
  import { onMount } from "svelte";
  import {
    AddGroup, AddRule, AddRuleSet, DeleteGroup, DeleteRule, DeleteRuleSet,
    EventsOn, GetMode, GetRouting, MoveRule, RefreshRuleSet, SetMode,
    SetRoutingFinal, SetRuleEnabled, UpdateGroup, UpdateRule, UpdateRuleSet,
  } from "$api";
  import RuleEditor from "../RuleEditor.svelte";
  import GroupsModal from "./GroupsModal.svelte";
  import RuleSetsModal from "./RuleSetsModal.svelte";
  import Icon from "../icons/Icon.svelte";
  import { reportError, showToast } from "../store";
  import { t, tr } from "../i18n";
  import type { rules } from "../../../wailsjs/go/models";

  let cfg: rules.Config = { version: 1, rules: [], groups: [], final: "proxy" } as rules.Config;
  let mode = "Rule";
  let editing: rules.Rule | null = null;
  /** Правило, для которого открыто меню действий; «» — меню закрыто. */
  let sheetID = "";
  let showGroups = false;
  let showSets = false;

  const MODES = ["Rule", "Global", "Direct"];

  onMount(async () => {
    await reload();
    EventsOn("core:mode", (m: string) => (mode = m));
  });

  async function reload() {
    try {
      cfg = await GetRouting();
      mode = await GetMode();
    } catch (e) {
      reportError(e);
    }
  }

  // Любое действие применяется к живому ядру сразу: Go проверяет конфиг и
  // перезапускает ядро, а при отказе откатывает правила — поэтому на ошибке мы
  // перечитываем состояние, а не оставляем на экране несохранённое.
  async function run(fn: () => Promise<unknown>, ok = "") {
    try {
      await fn();
      await reload();
      if (ok) showToast(ok);
    } catch (e) {
      reportError(e);
      await reload();
    }
  }

  const changeMode = (m: string) => run(() => SetMode(m));
  const changeFinal = (f: string) => run(() => SetRoutingFinal(f), tr("routing.finalSaved"));
  const toggleRule = (r: rules.Rule) => run(() => SetRuleEnabled(r.id, !r.enabled));

  function newRule() {
    editing = {
      id: "", name: "", enabled: true, match: "domainSuffix", values: [],
      action: "proxy", target: "", method: "", tlsFragment: false, builtin: false,
    } as rules.Rule;
  }

  async function saveRule(e: CustomEvent<rules.Rule>) {
    const r = e.detail;
    editing = null;
    await run(() => (r.id ? UpdateRule(r) : AddRule(r)), tr("routing.ruleSaved"));
  }

  function act(fn: () => Promise<unknown>, ok = "") {
    sheetID = "";
    return run(fn, ok);
  }

  const move = (id: string, index: number) => act(() => MoveRule(id, index));

  // --- группы и наборы ---
  const saveGroup = (g: rules.Group) =>
    run(() => (g.id ? UpdateGroup(g) : AddGroup(g)), tr("routing.groupSaved"));
  const removeGroup = (id: string) => run(() => DeleteGroup(id), tr("routing.groupDeleted"));
  const saveSet = (s: rules.RuleSet) =>
    run(() => (s.id ? UpdateRuleSet(s) : AddRuleSet(s)), tr("routing.setSaved"));
  const removeSet = (id: string) => run(() => DeleteRuleSet(id), tr("routing.setDeleted"));

  async function refreshSet(s: rules.RuleSet) {
    try {
      const n = await RefreshRuleSet(s.id);
      showToast(tr("routing.setRefreshed", { tag: s.tag, n }));
    } catch (e) {
      reportError(e);
    }
  }

  // Имена встроенных правил приходят из Go по-русски (их пишет rules.Defaults) —
  // переводим по самому имени, как это делает десктопная вкладка.
  const BUILTIN_NAMES: Record<string, string> = {
    "Блокировка рекламы": "builtin.ads",
    "Приватные сети (LAN, роутер)": "builtin.private",
    "Россия — напрямую": "builtin.ru",
  };

  // Обе читают $t внутри реактивного выражения: обычная функция посчиталась бы
  // один раз и застыла на языке первого кадра.
  $: ruleTitle = (r: rules.Rule): string => {
    if (r.builtin && BUILTIN_NAMES[r.name]) return $t(BUILTIN_NAMES[r.name]);
    return r.name || $t("match." + r.match);
  };

  $: ruleSummary = (r: rules.Rule): string => {
    if (r.match === "private") return $t("routing.private");
    const v = r.values || [];
    if (v.length > 2) {
      return v.slice(0, 2).join(", ") + " " + $t("routing.andMore", { n: v.length - 2 });
    }
    return v.join(", ");
  };

  $: sheetIndex = cfg.rules.findIndex((r) => r.id === sheetID);
  $: sheetRule = sheetIndex >= 0 ? cfg.rules[sheetIndex] : null;
</script>

<div class="wrap">
  <header>
    <h1>{$t("tab.routing")}</h1>
    <button class="btn primary sm" on:click={newRule}>
      <Icon name="plus" size={15} />{$t("routing.addRule")}
    </button>
  </header>

  <div class="segmented wide">
    {#each MODES as m}
      <button class:on={mode === m} on:click={() => changeMode(m)}>{$t("mode." + m)}</button>
    {/each}
  </div>
  <div class="hint mode-hint">{$t("mode." + mode + ".hint")}</div>

  <!-- В режимах «Глобально» и «Напрямую» ядро список игнорирует — гасим его
       целиком, чтобы не казалось, будто правила всё ещё работают. -->
  <ul class="list" class:off={mode !== "Rule"}>
    {#each cfg.rules as r (r.id)}
      <li class="row" class:disabled={!r.enabled}>
        <label class="check" aria-label={ruleTitle(r)}>
          <input type="checkbox" checked={r.enabled} on:change={() => toggleRule(r)} />
        </label>
        <button class="pick" on:click={() => (editing = r)}>
          <span class="txt">
            <span class="rname">
              {ruleTitle(r)}
              {#if r.builtin}<span class="pill">{$t("routing.builtin")}</span>{/if}
              {#if r.tlsFragment}<span class="pill warn">{$t("routing.frag")}</span>{/if}
            </span>
            <span class="rmeta">{$t("match." + r.match)}: {ruleSummary(r)}</span>
          </span>
          <span class="act {r.action}">{$t("action." + r.action)}</span>
        </button>
        <button class="more" aria-label={$t("common.edit")} on:click={() => (sheetID = r.id)}>
          <Icon name="drag" size={18} />
        </button>
      </li>
    {/each}
    {#if cfg.rules.length === 0}
      <li class="none">{$t("routing.empty")}</li>
    {/if}
  </ul>

  <div class="final panel">
    <span class="flbl">{$t("routing.final")}</span>
    <div class="segmented">
      {#each ["proxy", "direct"] as f}
        <button class:on={cfg.final === f} on:click={() => changeFinal(f)}>
          {$t("routing.final." + f)}
        </button>
      {/each}
    </div>
  </div>

  <div class="extras">
    <button class="btn" on:click={() => (showGroups = true)}>
      <Icon name="layers" size={15} />{$t("routing.groups")}
      {#if cfg.groups?.length}<span class="cnt">{cfg.groups.length}</span>{/if}
    </button>
    <button class="btn" on:click={() => (showSets = true)}>
      <Icon name="shield" size={15} />{$t("routing.sets")}
      {#if cfg.ruleSets?.length}<span class="cnt">{cfg.ruleSets.length}</span>{/if}
    </button>
  </div>
</div>

<!-- Меню действий над правилом: перестановка, включение и удаление. Правка — по
     нажатию на саму строку, это то, чего от списка ждут в первую очередь. -->
{#if sheetRule}
  <div class="modal-backdrop sheet-back" role="button" tabindex="0"
       on:click={() => (sheetID = "")}
       on:keydown={(e) => e.key === "Escape" && (sheetID = "")}>
    <div class="sheet" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="sheet-h">{ruleTitle(sheetRule)}</div>
      <button class="sact" disabled={sheetIndex === 0}
              on:click={() => move(sheetRule.id, sheetIndex - 1)}>
        <Icon name="arrowUp" size={17} />{$t("m.routing.up")}
      </button>
      <button class="sact" disabled={sheetIndex === cfg.rules.length - 1}
              on:click={() => move(sheetRule.id, sheetIndex + 1)}>
        <Icon name="arrowDown" size={17} />{$t("m.routing.down")}
      </button>
      <button class="sact" on:click={() => { const r = sheetRule; sheetID = ""; editing = r; }}>
        <Icon name="edit" size={17} />{$t("common.edit")}
      </button>
      <button class="sact danger" disabled={sheetRule.builtin}
              on:click={() => act(() => DeleteRule(sheetRule.id), tr("routing.ruleDeleted"))}>
        <Icon name="trash" size={17} />{$t("common.delete")}
      </button>
      {#if sheetRule.builtin}
        <div class="sheet-hint">{$t("m.routing.builtinLock")}</div>
      {/if}
    </div>
  </div>
{/if}

{#if editing}
  <!-- processPicker не передаём: WinAPI-пикера на Android нет, и вместе с ним
       из формы пропадают матчеры по процессу. -->
  <RuleEditor rule={editing} groups={cfg.groups} ruleSets={cfg.ruleSets || []}
              on:save={saveRule} on:cancel={() => (editing = null)} />
{/if}

{#if showGroups}
  <GroupsModal groups={cfg.groups || []} on:save={(e) => saveGroup(e.detail)}
               on:delete={(e) => removeGroup(e.detail)} on:close={() => (showGroups = false)} />
{/if}

{#if showSets}
  <RuleSetsModal sets={cfg.ruleSets || []} on:save={(e) => saveSet(e.detail)}
                 on:delete={(e) => removeSet(e.detail)} on:refresh={(e) => refreshSet(e.detail)}
                 on:close={() => (showSets = false)} />
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

  .mode-hint {
    margin-top: -6px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }
  .list.off {
    opacity: 0.45;
  }

  .row {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--line);
    border-radius: var(--r-3);
    background: var(--panel);
    overflow: hidden;
  }
  .row.disabled .txt {
    opacity: 0.5;
  }

  .check {
    flex: none;
    gap: 0;
    padding: 0 4px 0 12px;
  }

  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--s-2);
    padding: 12px var(--s-2) 12px var(--s-2);
    border: none;
    background: transparent;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .rname {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .rmeta {
    font-size: 11.5px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .act {
    flex: none;
    font-size: 10.5px;
    font-weight: 700;
    padding: 3px 9px;
    border-radius: var(--r-pill);
    background: var(--line);
    color: var(--text-2);
  }
  .act.proxy {
    color: var(--accent-2);
  }
  .act.direct {
    color: var(--ok);
  }
  .act.block {
    color: var(--danger);
  }

  .more {
    flex: none;
    width: 44px;
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

  .none {
    padding: var(--s-5) var(--s-3);
    text-align: center;
    font-size: 13px;
    color: var(--muted);
  }

  .final {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s-3);
    padding: 10px 12px;
  }
  .flbl {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-2);
  }

  .extras {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--s-2);
  }
  .cnt {
    background: var(--accent-dim);
    color: var(--accent-2);
    border-radius: var(--r-pill);
    padding: 0 6px;
    font-size: 10px;
    font-weight: 700;
  }

  /* Лист действий — как у профилей: прижат к низу, туда дотягивается палец. */
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
  .sheet-hint {
    padding: 4px var(--s-3) var(--s-2);
    font-size: 11.5px;
    color: var(--muted);
  }
  .sact {
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
  .sact:active:not(:disabled) {
    background: var(--panel-2);
  }
  .sact:disabled {
    color: var(--muted);
  }
  .sact.danger:not(:disabled) {
    color: var(--danger);
  }
</style>
