<script lang="ts">
  // Вкладка маршрутизации: режим, упорядоченный список правил и группы нод.
  // Порядок правил = приоритет, поэтому список перетаскиваемый.
  import { onMount } from "svelte";
  import {
    GetRouting, AddRule, UpdateRule, DeleteRule, MoveRule, SetRuleEnabled,
    SetRoutingFinal, AddGroup, UpdateGroup, DeleteGroup, GetMode, SetMode,
    AddRuleSet, UpdateRuleSet, DeleteRuleSet,
  } from "../../wailsjs/go/main/App";
  import { EventsOn } from "../../wailsjs/runtime/runtime";
  import RuleEditor from "./RuleEditor.svelte";
  import DomainCheck from "./DomainCheck.svelte";
  import { reportError, showToast } from "./store";
  import type { rules } from "../../wailsjs/go/models";

  let cfg: rules.Config = { version: 1, rules: [], groups: [], final: "proxy" } as rules.Config;
  let mode = "Rule";
  let editing: rules.Rule | null = null;
  let dragID = "";

  // Группы редактируются прямо в списке — форма на четыре поля не стоит модалки.
  let newGroup: rules.Group | null = null;
  // Удалённые наборы правил — там же: тег, адрес и как часто обновлять.
  let newSet: rules.RuleSet | null = null;
  let showSets = false;

  const matchLabels: Record<string, string> = {
    domainSuffix: "домен+поддомены", domain: "домен", domainKeyword: "часть домена",
    domainRegex: "регулярка", ipCIDR: "IP/подсеть", port: "порт",
    process: "процесс", processPath: "путь к программе", ruleSet: "набор",
    private: "приватные сети", protocol: "протокол", network: "тип трафика",
  };
  const actionLabels: Record<string, string> = {
    proxy: "через прокси", direct: "напрямую", block: "блок",
  };

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

  // Все действия применяются к живому ядру сразу: бэкенд проверяет конфиг
  // через `sing-box check` и перезапускает ядро, не снимая системный прокси.
  async function run(fn: () => Promise<unknown>, ok = "") {
    try {
      await fn();
      await reload();
      if (ok) showToast(ok);
    } catch (e) {
      reportError(e);
      await reload(); // откатываем UI к тому, что реально сохранилось
    }
  }

  const changeMode = (m: string) => run(() => SetMode(m));
  const changeFinal = (f: string) => run(() => SetRoutingFinal(f), "Поведение по умолчанию изменено");
  const toggleRule = (r: rules.Rule) => run(() => SetRuleEnabled(r.id, !r.enabled));
  const removeRule = (r: rules.Rule) => run(() => DeleteRule(r.id), "Правило удалено");

  function newRule() {
    editing = {
      id: "", name: "", enabled: true, match: "domainSuffix", values: [],
      action: "proxy", target: "", method: "", tlsFragment: false, builtin: false,
    } as rules.Rule;
  }

  async function saveRule(e: CustomEvent<rules.Rule>) {
    const r = e.detail;
    editing = null;
    await run(() => (r.id ? UpdateRule(r) : AddRule(r)), "Правило сохранено");
  }

  // --- перетаскивание ---
  function onDrop(target: rules.Rule) {
    if (!dragID || dragID === target.id) return;
    const index = cfg.rules.findIndex((r) => r.id === target.id);
    const id = dragID;
    dragID = "";
    run(() => MoveRule(id, index));
  }

  // --- группы ---
  function addGroupDraft() {
    newGroup = { id: "", name: "", type: "urltest", filter: "", nodes: [] } as rules.Group;
  }
  async function saveGroup(g: rules.Group) {
    newGroup = null;
    await run(() => (g.id ? UpdateGroup(g) : AddGroup(g)), "Группа сохранена");
  }
  const removeGroup = (g: rules.Group) => run(() => DeleteGroup(g.id), "Группа удалена");

  // --- удалённые наборы правил ---
  function addSetDraft() {
    showSets = true;
    newSet = { id: "", tag: "", type: "remote", url: "", format: "binary", updateHours: 24, detour: "direct" } as rules.RuleSet;
  }
  async function saveSet(s: rules.RuleSet) {
    newSet = null;
    await run(() => (s.id ? UpdateRuleSet(s) : AddRuleSet(s)), "Набор сохранён");
  }
  const removeSet = (s: rules.RuleSet) => run(() => DeleteRuleSet(s.id), "Набор удалён");

  function ruleSummary(r: rules.Rule): string {
    if (r.match === "private") return "локальная сеть и роутер";
    const v = r.values || [];
    return v.length > 3 ? `${v.slice(0, 3).join(", ")} и ещё ${v.length - 3}` : v.join(", ");
  }
</script>

<div class="wrap">
  <div class="modes">
    <span class="lbl">Режим:</span>
    {#each [["Rule", "По правилам"], ["Global", "Всё через прокси"], ["Direct", "Всё напрямую"]] as [id, label]}
      <button class="tab" class:on={mode === id} on:click={() => changeMode(id)}>{label}</button>
    {/each}
    <span class="hint mode-hint">
      {mode === "Rule" ? "Работает список ниже" : "Список правил временно игнорируется"}
    </span>
  </div>

  <DomainCheck />

  <div class="head">
    <span class="panel-h">Правила — сверху вниз, срабатывает первое подходящее</span>
    <button class="mini wide" on:click={newRule}>+ Правило</button>
  </div>

  <div class="list" class:off={mode !== "Rule"}>
    {#each cfg.rules as r (r.id)}
      <div class="rule" class:disabled={!r.enabled}
           draggable="true"
           on:dragstart={() => (dragID = r.id)}
           on:dragover|preventDefault
           on:drop|preventDefault={() => onDrop(r)}>
        <span class="grip" title="Перетащи, чтобы изменить приоритет">⋮⋮</span>
        <input type="checkbox" checked={r.enabled} on:change={() => toggleRule(r)} />
        <div class="body">
          <div class="name">
            {r.name || matchLabels[r.match] || r.match}
            {#if r.builtin}<span class="tag">встроенное</span>{/if}
            {#if r.tlsFragment}<span class="tag frag">фрагментация</span>{/if}
          </div>
          <div class="meta">
            {matchLabels[r.match] || r.match}: {ruleSummary(r)}
          </div>
        </div>
        <span class="act {r.action}">
          {actionLabels[r.action] || r.action}{r.target ? " → " + r.target : ""}
        </span>
        <button class="mini" title="Изменить" on:click={() => (editing = r)}>✎</button>
        <button class="mini danger" title="Удалить" disabled={r.builtin}
                on:click={() => removeRule(r)}>✕</button>
      </div>
    {/each}
    {#if cfg.rules.length === 0}
      <div class="empty">Правил нет — весь трафик идёт по умолчанию.</div>
    {/if}
  </div>

  <label class="final">
    Остальной трафик:
    <select class="fld" value={cfg.final} on:change={(e) => changeFinal(e.currentTarget.value)}>
      <option value="proxy">через прокси</option>
      <option value="direct">напрямую</option>
    </select>
  </label>

  <div class="head">
    <span class="panel-h">Группы нод</span>
    <button class="mini wide" on:click={addGroupDraft}>+ Группа</button>
  </div>

  <div class="groups">
    {#each cfg.groups as g (g.id)}
      <div class="group">
        <input class="fld gname" bind:value={g.name} on:change={() => saveGroup(g)} />
        <select class="fld" bind:value={g.type} on:change={() => saveGroup(g)}>
          <option value="urltest">по задержке</option>
          <option value="select">ручной выбор</option>
        </select>
        <input class="fld gfilter" bind:value={g.filter} on:change={() => saveGroup(g)}
               placeholder="фильтр по имени ноды, напр. ^NL" />
        <button class="mini danger" title="Удалить группу" on:click={() => removeGroup(g)}>✕</button>
      </div>
    {/each}

    {#if newGroup}
      <div class="group">
        <input class="fld gname" bind:value={newGroup.name} placeholder="Название" />
        <select class="fld" bind:value={newGroup.type}>
          <option value="urltest">по задержке</option>
          <option value="select">ручной выбор</option>
        </select>
        <input class="fld gfilter" bind:value={newGroup.filter} placeholder="^NL" />
        <button class="mini" title="Сохранить" on:click={() => saveGroup(newGroup)}>✓</button>
        <button class="mini danger" title="Отмена" on:click={() => (newGroup = null)}>✕</button>
      </div>
    {/if}

    {#if cfg.groups.length === 0 && !newGroup}
      <div class="hint">
        Группа собирает ноды по фильтру имени и появляется отдельным пунктом в списке
        выбора — на неё можно направить отдельные правила.
      </div>
    {/if}
  </div>

  <div class="head sets-head">
    <button class="panel-h link" on:click={() => (showSets = !showSets)}>
      {showSets ? "▾" : "▸"} Наборы правил{cfg.ruleSets?.length ? ` (${cfg.ruleSets.length})` : ""}
    </button>
    <button class="mini wide" on:click={addSetDraft}>+ Набор</button>
  </div>

  {#if showSets}
    <div class="groups">
      {#each cfg.ruleSets || [] as s (s.id)}
        <div class="group set">
          <input class="fld gname" bind:value={s.tag} on:change={() => saveSet(s)} />
          <input class="fld gfilter" bind:value={s.url} on:change={() => saveSet(s)}
                 placeholder="https://…/list.srs" />
          <select class="fld sfmt" bind:value={s.format} on:change={() => saveSet(s)}>
            <option value="binary">.srs</option>
            <option value="source">json</option>
          </select>
          <select class="fld sfmt" bind:value={s.detour} on:change={() => saveSet(s)}>
            <option value="direct">качать напрямую</option>
            <option value="proxy">качать через прокси</option>
          </select>
          <button class="mini danger" title="Удалить набор" on:click={() => removeSet(s)}>✕</button>
        </div>
      {/each}

      {#if newSet}
        <div class="group set">
          <input class="fld gname" bind:value={newSet.tag} placeholder="имя-набора" />
          <input class="fld gfilter" bind:value={newSet.url} placeholder="https://…/list.srs" />
          <select class="fld sfmt" bind:value={newSet.format}>
            <option value="binary">.srs</option>
            <option value="source">json</option>
          </select>
          <select class="fld sfmt" bind:value={newSet.detour}>
            <option value="direct">качать напрямую</option>
            <option value="proxy">качать через прокси</option>
          </select>
          <button class="mini" title="Сохранить" on:click={() => saveSet(newSet)}>✓</button>
          <button class="mini danger" title="Отмена" on:click={() => (newSet = null)}>✕</button>
        </div>
      {/if}

      <div class="hint">
        Ядро само качает набор по адресу и обновляет раз в сутки, храня его в кэше.
        Имя набора появится в правиле «Готовый набор». Если источник заблокирован —
        поставь «качать через прокси».
      </div>
    </div>
  {/if}
</div>

{#if editing}
  <RuleEditor rule={editing} groups={cfg.groups} ruleSets={cfg.ruleSets || []}
              on:save={saveRule} on:cancel={() => (editing = null)} />
{/if}

<style>
  .wrap { display: flex; flex-direction: column; gap: 10px; min-height: 0; flex: 1; }
  .modes { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .modes .lbl { font-size: 13px; color: var(--muted); }
  .mode-hint { margin-left: auto; }
  .tab {
    background: var(--bg); border: 1px solid var(--line); color: var(--text-2);
    padding: 5px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit;
  }
  .tab.on { background: #1f6feb; border-color: #1f6feb; color: #fff; font-weight: 700; }

  .head { display: flex; align-items: center; justify-content: space-between; }
  .head .panel-h { margin: 0; }

  .list { display: flex; flex-direction: column; gap: 5px; overflow-y: auto; min-height: 80px; }
  .list.off { opacity: 0.45; }
  .rule {
    display: flex; align-items: center; gap: 8px; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 7px 10px;
  }
  .rule.disabled { opacity: 0.5; }
  .grip { color: var(--muted-2); cursor: grab; font-size: 12px; letter-spacing: -2px; }
  .body { flex: 1; min-width: 0; text-align: left; }
  .name { font-size: 13px; font-weight: 700; display: flex; align-items: center; gap: 6px; }
  .meta {
    font-size: 11px; color: var(--muted); overflow: hidden;
    text-overflow: ellipsis; white-space: nowrap;
  }
  .tag {
    font-size: 10px; font-weight: 700; padding: 1px 6px; border-radius: 999px;
    background: var(--line); color: var(--muted);
  }
  .tag.frag { background: #3a2d00; color: var(--yellow); }
  .act {
    font-size: 11px; font-weight: 700; padding: 3px 9px; border-radius: 999px;
    background: #21262d; color: var(--text-2); flex: none;
  }
  .act.proxy { color: #58a6ff; }
  .act.direct { color: var(--green); }
  .act.block { color: var(--red); }

  .final { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-2); }

  .groups { display: flex; flex-direction: column; gap: 6px; }
  .group { display: flex; align-items: center; gap: 6px; }
  .gname { width: 150px; }
  .gfilter { flex: 1; min-width: 0; font-family: ui-monospace, Consolas, monospace; font-size: 12px; }
  .sfmt { flex: none; width: auto; font-size: 12px; }
  .sets-head .link {
    background: none; border: 0; color: var(--muted); font: inherit; font-weight: 700;
    font-size: 13px; cursor: pointer; padding: 0; margin: 0;
  }
</style>
