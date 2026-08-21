<script lang="ts">
  // Удалённые наборы правил на телефоне. Как и группы — карточкой в столбик:
  // на ПК тег, адрес, формат и способ закачки стоят в одну строку.
  //
  // Новый набор уходит в Go только по «Добавить»: App.AddRuleSet качает список
  // до записи в routing.json, и промежуточные состояния адреса ему ни к чему.
  import { createEventDispatcher } from "svelte";
  import Icon from "../icons/Icon.svelte";
  import { t } from "../i18n";
  import type { rules } from "../../../wailsjs/go/models";

  export let sets: rules.RuleSet[] = [];

  const dispatch = createEventDispatcher<{
    save: rules.RuleSet;
    delete: string;
    refresh: rules.RuleSet;
    close: void;
  }>();

  let draft: rules.RuleSet | null = null;

  function addDraft() {
    draft = {
      id: "", tag: "", type: "remote", url: "", format: "binary",
      updateHours: 24, detour: "direct",
    } as rules.RuleSet;
  }

  function saveDraft() {
    if (!draft || !draft.tag.trim() || !draft.url.trim()) return;
    dispatch("save", draft);
    draft = null;
  }
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {$t("routing.sets")}
      <button class="icon-btn" on:click={() => dispatch("close")} aria-label={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="modal-b">
      {#each sets as s (s.id)}
        <div class="card">
          <label class="f">
            <span class="k">{$t("m.routing.setTag")}</span>
            <input class="fld" bind:value={s.tag} on:change={() => dispatch("save", s)} />
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.setURL")}</span>
            <input class="fld mono" bind:value={s.url} on:change={() => dispatch("save", s)}
                   placeholder={$t("routing.set.urlPh")} />
          </label>
          <div class="two">
            <label class="f">
              <span class="k">{$t("m.routing.setFormat")}</span>
              <select class="fld" bind:value={s.format} on:change={() => dispatch("save", s)}>
                <option value="binary">{$t("routing.set.binary")}</option>
                <option value="source">{$t("routing.set.source")}</option>
                <option value="lst">{$t("routing.set.lst")}</option>
              </select>
            </label>
            <label class="f">
              <span class="k">{$t("m.routing.setDetour")}</span>
              <select class="fld" bind:value={s.detour} on:change={() => dispatch("save", s)}>
                <option value="direct">{$t("routing.set.direct")}</option>
                <option value="proxy">{$t("routing.set.proxy")}</option>
              </select>
            </label>
          </div>
          <div class="acts">
            <!-- Списки .lst качаем и конвертируем мы сами, поэтому их можно
                 обновить руками; .srs и json ядро тянет по своему расписанию. -->
            {#if s.format === "lst"}
              <button class="btn sm" on:click={() => dispatch("refresh", s)}>
                <Icon name="refresh" size={13} />{$t("common.refresh")}
              </button>
            {/if}
            <button class="btn sm danger-b" on:click={() => dispatch("delete", s.id)}>
              <Icon name="trash" size={13} />{$t("common.delete")}
            </button>
          </div>
        </div>
      {/each}

      {#if draft}
        <div class="card">
          <label class="f">
            <span class="k">{$t("m.routing.setTag")}</span>
            <input class="fld" bind:value={draft.tag} placeholder={$t("routing.set.tagPh")} />
          </label>
          <label class="f">
            <span class="k">{$t("m.routing.setURL")}</span>
            <input class="fld mono" bind:value={draft.url} placeholder={$t("routing.set.urlPh")} />
          </label>
          <div class="two">
            <label class="f">
              <span class="k">{$t("m.routing.setFormat")}</span>
              <select class="fld" bind:value={draft.format}>
                <option value="binary">{$t("routing.set.binary")}</option>
                <option value="source">{$t("routing.set.source")}</option>
                <option value="lst">{$t("routing.set.lst")}</option>
              </select>
            </label>
            <label class="f">
              <span class="k">{$t("m.routing.setDetour")}</span>
              <select class="fld" bind:value={draft.detour}>
                <option value="direct">{$t("routing.set.direct")}</option>
                <option value="proxy">{$t("routing.set.proxy")}</option>
              </select>
            </label>
          </div>
          <div class="acts end">
            <button class="btn sm" on:click={() => (draft = null)}>{$t("common.cancel")}</button>
            <button class="btn sm primary" on:click={saveDraft}>{$t("common.add")}</button>
          </div>
        </div>
      {/if}

      <div class="hint">{$t("m.routing.setsHint")}</div>
    </div>

    <div class="modal-f">
      <button class="btn" on:click={addDraft} disabled={!!draft}>
        <Icon name="plus" size={14} />{$t("routing.addSet")}
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
    min-width: 0;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--s-2);
  }
  .k {
    font-size: 11px;
    color: var(--muted);
  }
  .mono {
    font-family: ui-monospace, Consolas, monospace;
    font-size: 12px;
  }

  .acts {
    display: flex;
    gap: var(--s-2);
  }
  .acts.end {
    justify-content: flex-end;
  }
  .danger-b {
    color: var(--danger);
  }
</style>
