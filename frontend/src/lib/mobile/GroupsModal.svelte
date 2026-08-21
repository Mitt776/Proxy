<script lang="ts">
  // Группы нод на телефоне: те же четыре поля, что и на ПК, но карточкой в
  // столбик — в десктопной строке они стоят в ряд и на 360 px превращаются в
  // четыре поля по восемьдесят пикселей.
  //
  // Сохранение по потере фокуса, как на ПК: каждая правка уходит в Go, тот
  // проверяет конфиг и перезапускает ядро.
  import { createEventDispatcher } from "svelte";
  import Icon from "../icons/Icon.svelte";
  import { t } from "../i18n";
  import type { rules } from "../../../wailsjs/go/models";

  export let groups: rules.Group[] = [];

  const dispatch = createEventDispatcher<{
    save: rules.Group;
    delete: string;
    close: void;
  }>();

  /** Новая группа правится тут же и уходит в Go только по «Добавить». */
  let draft: rules.Group | null = null;

  function addDraft() {
    draft = { id: "", name: "", type: "urltest", filter: "", nodes: [] } as rules.Group;
  }

  function saveDraft() {
    if (!draft || !draft.name.trim()) return;
    dispatch("save", draft);
    draft = null;
  }
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {$t("routing.groups")}
      <button class="icon-btn" on:click={() => dispatch("close")} aria-label={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="modal-b">
      {#each groups as g (g.id)}
        <div class="card">
          <label class="f">
            <span class="k">{$t("routing.group.name")}</span>
            <input class="fld" bind:value={g.name} on:change={() => dispatch("save", g)} />
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.groupType")}</span>
            <select class="fld" bind:value={g.type} on:change={() => dispatch("save", g)}>
              <option value="urltest">{$t("routing.group.urltest")}</option>
              <option value="select">{$t("routing.group.select")}</option>
            </select>
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.groupFilter")}</span>
            <input class="fld mono" bind:value={g.filter} on:change={() => dispatch("save", g)}
                   placeholder={$t("routing.group.filterPh")} />
          </label>
          <button class="btn sm danger-b" on:click={() => dispatch("delete", g.id)}>
            <Icon name="trash" size={13} />{$t("common.delete")}
          </button>
        </div>
      {/each}

      {#if draft}
        <div class="card">
          <label class="f">
            <span class="k">{$t("routing.group.name")}</span>
            <input class="fld" bind:value={draft.name} placeholder={$t("routing.group.name")} />
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.groupType")}</span>
            <select class="fld" bind:value={draft.type}>
              <option value="urltest">{$t("routing.group.urltest")}</option>
              <option value="select">{$t("routing.group.select")}</option>
            </select>
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.groupFilter")}</span>
            <input class="fld mono" bind:value={draft.filter} placeholder="^NL" />
          </label>
          <div class="draft-f">
            <button class="btn sm" on:click={() => (draft = null)}>{$t("common.cancel")}</button>
            <button class="btn sm primary" on:click={saveDraft}>{$t("common.add")}</button>
          </div>
        </div>
      {/if}

      <div class="hint">{$t("routing.groupsHint")}</div>
    </div>

    <div class="modal-f">
      <button class="btn" on:click={addDraft} disabled={!!draft}>
        <Icon name="plus" size={14} />{$t("routing.addGroup")}
      </button>
      <button class="btn primary" on:click={() => dispatch("close")}>{$t("common.close")}</button>
    </div>
  </div>
</div>

<style>
  .modal-b {
    display: flex;
    flex-direction: column;
    gap: var(--s-3);
  }
  .modal-f {
    justify-content: space-between;
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    padding: var(--s-3);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: var(--r-3);
  }
  .f {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .k {
    font-size: 11px;
    color: var(--muted);
  }
  .mono {
    font-family: ui-monospace, Consolas, monospace;
    font-size: 12px;
  }

  .danger-b {
    align-self: flex-start;
    color: var(--danger);
  }
  .draft-f {
    display: flex;
    justify-content: flex-end;
    gap: var(--s-2);
  }
</style>
