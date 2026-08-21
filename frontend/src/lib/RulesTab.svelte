<script lang="ts">
  // Вкладка маршрутизации: режим, упорядоченный список правил и «остальной
  // трафик». Порядок правил = приоритет, поэтому список перетаскиваемый.
  // Группы нод, удалённые наборы и проверка домена нужны редко — они за
  // кнопками в шапке, чтобы главный список ничем не перебивался.
  import { onMount } from "svelte";
  import {
    GetRouting, AddRule, UpdateRule, DeleteRule, MoveRule, SetRuleEnabled,
    SetRoutingFinal, AddGroup, UpdateGroup, DeleteGroup, GetMode, SetMode,
    AddRuleSet, UpdateRuleSet, DeleteRuleSet, RefreshRuleSet,
  } from "$api";
  import { EventsOn } from "$api";
  import RuleEditor from "./RuleEditor.svelte";
  import DomainCheck from "./DomainCheck.svelte";
  import Icon from "./icons/Icon.svelte";
  import TabHead from "./shell/TabHead.svelte";
  import { reportError, showToast } from "./store";
  import { t, tr } from "./i18n";
  import type { rules } from "../../wailsjs/go/models";

  let cfg: rules.Config = { version: 1, rules: [], groups: [], final: "proxy" } as rules.Config;
  let mode = "Rule";
  let editing: rules.Rule | null = null;
  let dragID = "";

  let showCheck = false;
  let showGroups = false;
  let showSets = false;

  // Группы и наборы правятся прямо в своих модалках — форма на четыре поля не
  // стоит ещё одного уровня вложенности.
  let newGroup: rules.Group | null = null;
  let newSet: rules.RuleSet | null = null;
  let refreshing = "";

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
  const changeFinal = (f: string) => run(() => SetRoutingFinal(f), tr("routing.finalSaved"));
  const toggleRule = (r: rules.Rule) => run(() => SetRuleEnabled(r.id, !r.enabled));
  const removeRule = (r: rules.Rule) => run(() => DeleteRule(r.id), tr("routing.ruleDeleted"));

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
    await run(() => (g.id ? UpdateGroup(g) : AddGroup(g)), tr("routing.groupSaved"));
  }
  const removeGroup = (g: rules.Group) => run(() => DeleteGroup(g.id), tr("routing.groupDeleted"));

  // --- удалённые наборы правил ---
  function addSetDraft() {
    newSet = {
      id: "", tag: "", type: "remote", url: "", format: "binary",
      updateHours: 24, detour: "direct",
    } as rules.RuleSet;
  }
  async function saveSet(s: rules.RuleSet) {
    newSet = null;
    await run(() => (s.id ? UpdateRuleSet(s) : AddRuleSet(s)), tr("routing.setSaved"));
  }
  const removeSet = (s: rules.RuleSet) => run(() => DeleteRuleSet(s.id), tr("routing.setDeleted"));

  // Ручное обновление списка: набор из .lst качает и конвертирует приложение,
  // и без кнопки пришлось бы ждать получасового тика планировщика.
  async function refreshSet(s: rules.RuleSet) {
    if (refreshing) return;
    refreshing = s.id;
    try {
      const n = await RefreshRuleSet(s.id);
      showToast(tr("routing.setRefreshed", { tag: s.tag, n }));
    } catch (e) {
      reportError(e);
    } finally {
      refreshing = "";
    }
  }

  // Имена встроенных правил приходят из Go по-русски (их пишет rules.Defaults).
  // Переводим по самому имени: своих идентификаторов у них нет, а переименованное
  // пользователем правило просто не найдётся в таблице и останется как есть.
  const BUILTIN_NAMES: Record<string, string> = {
    "Блокировка рекламы": "builtin.ads",
    "Приватные сети (LAN, роутер)": "builtin.private",
    "Россия — напрямую": "builtin.ru",
  };

  // Обе функции читают $t, а не tr: иначе список, отрисованный до того, как язык
  // приехал из Go, так и остался бы на языке первого кадра.
  $: ruleTitle = (r: rules.Rule): string => {
    if (r.builtin && BUILTIN_NAMES[r.name]) return $t(BUILTIN_NAMES[r.name]);
    return r.name || $t("match." + r.match);
  };

  $: ruleSummary = (r: rules.Rule): string => {
    if (r.match === "private") return $t("routing.private");
    const v = r.values || [];
    if (v.length > 3) {
      return v.slice(0, 3).join(", ") + " " + $t("routing.andMore", { n: v.length - 3 });
    }
    return v.join(", ");
  };
</script>

<div class="tab-wrap">
  <TabHead title={$t("tab.routing")} sub={$t("routing.subtitle")}>
    <button class="btn ghost sm" on:click={() => (showCheck = true)}>
      <Icon name="search" size={14} />{$t("check.title")}
    </button>
    <button class="btn ghost sm" on:click={() => (showGroups = true)}>
      <Icon name="layers" size={14} />{$t("routing.groups")}
      {#if cfg.groups?.length}<span class="cnt">{cfg.groups.length}</span>{/if}
    </button>
    <button class="btn ghost sm" on:click={() => (showSets = true)}>
      <Icon name="shield" size={14} />{$t("routing.sets")}
      {#if cfg.ruleSets?.length}<span class="cnt">{cfg.ruleSets.length}</span>{/if}
    </button>
    <button class="btn primary" on:click={newRule}>
      <Icon name="plus" size={15} />{$t("routing.addRule")}
    </button>
  </TabHead>

  <div class="modebar">
    <div class="segmented">
      {#each MODES as m}
        <button class:on={mode === m} title={$t("mode." + m + ".hint")}
                on:click={() => changeMode(m)}>{$t("mode." + m + ".long")}</button>
      {/each}
    </div>
    <span class="hint">{mode === "Rule" ? $t("routing.modeHint.on") : $t("routing.modeHint.off")}</span>
  </div>

  <div class="rows" class:off={mode !== "Rule"}>
    {#each cfg.rules as r (r.id)}
      <div class="row-i rule" class:disabled={!r.enabled}
           draggable="true"
           on:dragstart={() => (dragID = r.id)}
           on:dragover|preventDefault
           on:drop|preventDefault={() => onDrop(r)}>
        <span class="grip" title={$t("routing.dragHint")}><Icon name="drag" size={14} /></span>
        <label class="check">
          <input type="checkbox" checked={r.enabled} on:change={() => toggleRule(r)} />
        </label>
        <div class="body">
          <div class="name">
            {ruleTitle(r)}
            {#if r.builtin}<span class="pill">{$t("routing.builtin")}</span>{/if}
            {#if r.tlsFragment}<span class="pill warn">{$t("routing.frag")}</span>{/if}
          </div>
          <div class="meta">{$t("match." + r.match)}: {ruleSummary(r)}</div>
        </div>
        <span class="act {r.action}">
          {$t("action." + r.action)}{r.target ? " → " + r.target : ""}
        </span>
        <button class="icon-btn" title={$t("common.edit")} on:click={() => (editing = r)}>
          <Icon name="edit" size={14} />
        </button>
        <button class="icon-btn danger" title={$t("common.delete")} disabled={r.builtin}
                on:click={() => removeRule(r)}>
          <Icon name="trash" size={14} />
        </button>
      </div>
    {/each}
    {#if cfg.rules.length === 0}
      <div class="empty">{$t("routing.empty")}</div>
    {/if}
  </div>

  <div class="final card">
    <Icon name="route" size={16} />
    <span class="flbl">{$t("routing.final")}</span>
    <div class="segmented">
      {#each ["proxy", "direct"] as f}
        <button class:on={cfg.final === f} on:click={() => changeFinal(f)}>
          {$t("routing.final." + f)}
        </button>
      {/each}
    </div>
  </div>
</div>

{#if showCheck}
  <DomainCheck on:close={() => (showCheck = false)} />
{/if}

{#if showGroups}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (showGroups = false)}
       on:keydown={(e) => e.key === "Escape" && (showGroups = false)}>
    <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("routing.groups")}
        <button class="icon-btn" on:click={() => (showGroups = false)} title={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>

      <div class="modal-b">
        {#each cfg.groups as g (g.id)}
          <div class="line">
            <input class="fld gname" bind:value={g.name} on:change={() => saveGroup(g)} />
            <select class="fld" bind:value={g.type} on:change={() => saveGroup(g)}>
              <option value="urltest">{$t("routing.group.urltest")}</option>
              <option value="select">{$t("routing.group.select")}</option>
            </select>
            <input class="fld mono" bind:value={g.filter} on:change={() => saveGroup(g)}
                   placeholder={$t("routing.group.filterPh")} />
            <button class="icon-btn danger" title={$t("common.delete")} on:click={() => removeGroup(g)}>
              <Icon name="trash" size={14} />
            </button>
          </div>
        {/each}

        {#if newGroup}
          <div class="line">
            <input class="fld gname" bind:value={newGroup.name} placeholder={$t("routing.group.name")} />
            <select class="fld" bind:value={newGroup.type}>
              <option value="urltest">{$t("routing.group.urltest")}</option>
              <option value="select">{$t("routing.group.select")}</option>
            </select>
            <input class="fld mono" bind:value={newGroup.filter} placeholder="^NL" />
            <button class="icon-btn" title={$t("common.save")} on:click={() => saveGroup(newGroup)}>
              <Icon name="check" size={14} />
            </button>
            <button class="icon-btn danger" title={$t("common.cancel")} on:click={() => (newGroup = null)}>
              <Icon name="close" size={14} />
            </button>
          </div>
        {/if}

        <div class="hint">{$t("routing.groupsHint")}</div>
      </div>

      <div class="modal-f">
        <button class="btn" on:click={addGroupDraft} disabled={!!newGroup}>
          <Icon name="plus" size={14} />{$t("routing.addGroup")}
        </button>
        <button class="btn primary" on:click={() => (showGroups = false)}>{$t("common.close")}</button>
      </div>
    </div>
  </div>
{/if}

{#if showSets}
  <div class="modal-backdrop" role="button" tabindex="0"
       on:click={() => (showSets = false)}
       on:keydown={(e) => e.key === "Escape" && (showSets = false)}>
    <div class="modal wide" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-h">
        {$t("routing.sets")}
        <button class="icon-btn" on:click={() => (showSets = false)} title={$t("common.close")}>
          <Icon name="close" size={15} />
        </button>
      </div>

      <div class="modal-b">
        {#each cfg.ruleSets || [] as s (s.id)}
          <div class="line">
            <input class="fld gname" bind:value={s.tag} on:change={() => saveSet(s)} />
            <input class="fld mono" bind:value={s.url} on:change={() => saveSet(s)}
                   placeholder={$t("routing.set.urlPh")} />
            <select class="fld sm" bind:value={s.format} on:change={() => saveSet(s)}>
              <option value="binary">{$t("routing.set.binary")}</option>
              <option value="source">{$t("routing.set.source")}</option>
              <option value="lst">{$t("routing.set.lst")}</option>
            </select>
            <select class="fld sm" bind:value={s.detour} on:change={() => saveSet(s)}>
              <option value="direct">{$t("routing.set.direct")}</option>
              <option value="proxy">{$t("routing.set.proxy")}</option>
            </select>
            <!-- Списки .lst качаем мы сами, поэтому их можно обновить руками;
                 .srs и json ядро тянет по своему расписанию. -->
            {#if s.format === "lst"}
              <button class="icon-btn" title={$t("routing.set.refresh")}
                      disabled={refreshing === s.id} on:click={() => refreshSet(s)}>
                <Icon name="refresh" size={14} />
              </button>
            {/if}
            <button class="icon-btn danger" title={$t("common.delete")} on:click={() => removeSet(s)}>
              <Icon name="trash" size={14} />
            </button>
          </div>
        {/each}

        {#if newSet}
          <div class="line">
            <input class="fld gname" bind:value={newSet.tag} placeholder={$t("routing.set.tagPh")} />
            <input class="fld mono" bind:value={newSet.url} placeholder={$t("routing.set.urlPh")} />
            <select class="fld sm" bind:value={newSet.format}>
              <option value="binary">{$t("routing.set.binary")}</option>
              <option value="source">{$t("routing.set.source")}</option>
              <option value="lst">{$t("routing.set.lst")}</option>
            </select>
            <select class="fld sm" bind:value={newSet.detour}>
              <option value="direct">{$t("routing.set.direct")}</option>
              <option value="proxy">{$t("routing.set.proxy")}</option>
            </select>
            <button class="icon-btn" title={$t("common.save")} on:click={() => saveSet(newSet)}>
              <Icon name="check" size={14} />
            </button>
            <button class="icon-btn danger" title={$t("common.cancel")} on:click={() => (newSet = null)}>
              <Icon name="close" size={14} />
            </button>
          </div>
        {/if}

        <div class="hint">{$t("routing.setsHint")}</div>
      </div>

      <div class="modal-f">
        <button class="btn" on:click={addSetDraft} disabled={!!newSet}>
          <Icon name="plus" size={14} />{$t("routing.addSet")}
        </button>
        <button class="btn primary" on:click={() => (showSets = false)}>{$t("common.close")}</button>
      </div>
    </div>
  </div>
{/if}

{#if editing}
  <RuleEditor rule={editing} groups={cfg.groups} ruleSets={cfg.ruleSets || []}
              on:save={saveRule} on:cancel={() => (editing = null)} />
{/if}

<style>
  .cnt {
    background: var(--accent-dim);
    color: var(--accent-2);
    border-radius: var(--r-pill);
    padding: 0 6px;
    font-size: 10px;
    font-weight: 700;
  }

  .modebar { display: flex; align-items: center; gap: var(--s-3); flex: none; }

  /* В режимах Global/Direct список ядром игнорируется — гасим его целиком,
     чтобы не создавать впечатления, будто правила работают. */
  .rows.off { opacity: 0.45; }

  .rule { cursor: grab; }
  .rule.disabled { opacity: 0.5; }
  .grip { display: flex; color: var(--muted); flex: none; }
  .check { gap: 0; }

  .body { flex: 1; min-width: 0; }
  .name {
    font-size: 13px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .meta {
    font-size: 11px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-top: 1px;
  }

  .act {
    font-size: 11px;
    font-weight: 700;
    padding: 3px 10px;
    border-radius: var(--r-pill);
    background: var(--line);
    color: var(--text-2);
    flex: none;
    max-width: 190px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .act.proxy { color: var(--accent-2); }
  .act.direct { color: var(--ok); }
  .act.block { color: var(--danger); }

  .final {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    padding: 10px 14px;
    flex: none;
    color: var(--text-2);
  }
  .flbl { font-size: 13px; font-weight: 600; margin-right: auto; }

  .modal-b { display: flex; flex-direction: column; gap: var(--s-2); }
  .modal.wide { max-width: 760px; }
  .line { display: flex; align-items: center; gap: 6px; }
  .gname { width: 150px; flex: none; }
  .mono { flex: 1; min-width: 0; font-family: ui-monospace, Consolas, monospace; font-size: 12px; }
  .fld.sm { flex: none; width: auto; font-size: 12px; }
  .modal-f { justify-content: space-between; }
</style>
