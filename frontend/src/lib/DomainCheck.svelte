<script lang="ts">
  // Проверка домена до подключения: куда он уйдёт по текущим правилам.
  // Считает бэкенд, спрашивая само ядро (`sing-box rule-set match`), поэтому
  // ответ совпадает с боевым поведением, а не с нашими догадками.
  import { createEventDispatcher } from "svelte";
  import { CheckDomain } from "../../wailsjs/go/main/App";
  import { t } from "./i18n";
  import { errText } from "./i18n/errors";
  import Icon from "./icons/Icon.svelte";
  import type { config } from "../../wailsjs/go/models";

  const dispatch = createEventDispatcher<{ close: void }>();

  let domain = "";
  let busy = false;
  let error = "";
  let result: config.DomainCheck | null = null;

  async function check() {
    if (!domain.trim() || busy) return;
    busy = true;
    error = "";
    try {
      result = await CheckDomain(domain);
    } catch (e) {
      result = null;
      error = errText(e);
    } finally {
      busy = false;
    }
  }

  // Показываем только то, что пользователю действительно стоит увидеть:
  // сработавшее правило и всё, что проверить не удалось.
  $: notable = (result?.steps || []).filter((s) => s.status !== "miss");
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      {$t("check.title")}
      <button class="icon-btn" on:click={() => dispatch("close")} title={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="modal-b">
      <div class="row">
        <!-- svelte-ignore a11y-autofocus -->
        <input class="fld" placeholder={$t("check.ph")} bind:value={domain} autofocus
               on:keydown={(e) => e.key === "Enter" && check()} />
        <button class="btn primary" on:click={check} disabled={busy || !domain.trim()}>
          {busy ? "…" : $t("check.run")}
        </button>
      </div>

      {#if error}
        <div class="error">{error}</div>
      {:else if result}
        <div class="verdict card">
          <div class="line">
            <span class="dom">{result.domain}</span>
            <span class="arrow">→</span>
            <span class="act {result.action}">
              {result.action === "block" ? $t("action.blocked") : $t("action." + result.action)}
            </span>
            {#if result.target}<span class="via">«{result.target}»</span>{/if}
          </div>
          <div class="why">
            {#if result.mode !== "Rule"}
              {$t("check.modeOverride", { mode: $t("mode." + result.mode + ".long") })}
            {:else if result.byFinal}
              {$t("check.byFinal")}
            {:else}
              {$t("check.byRule", { name: result.ruleTitle })}
            {/if}
          </div>
        </div>

        {#if notable.length}
          <div class="steps">
            {#each notable as s}
              <div class="step">
                <span class="pill" class:ok={s.status === "match"} class:warn={s.status === "unknown"}>
                  {$t("check.status." + (s.status === "match" || s.status === "unknown" ? s.status : "skip"))}
                </span>
                <span class="title">{s.title}</span>
                {#if s.reason}<span class="reason">{s.reason}</span>{/if}
              </div>
            {/each}
          </div>
        {/if}

        <div class="hint">{$t("check.hint")}</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .modal { max-width: 560px; }
  .modal-b { display: flex; flex-direction: column; gap: var(--s-3); }

  .row { display: flex; gap: var(--s-2); }
  .row .fld { flex: 1; min-width: 0; }

  .verdict { display: flex; flex-direction: column; gap: 5px; }
  .line { display: flex; align-items: baseline; gap: var(--s-2); flex-wrap: wrap; font-size: 14px; }
  .dom { font-weight: 700; }
  .arrow { color: var(--muted); }
  .act { font-weight: 800; }
  .act.proxy { color: var(--accent-2); }
  .act.direct { color: var(--ok); }
  .act.block { color: var(--danger); }
  .via { color: var(--text-2); font-size: 12.5px; }
  .why { font-size: 11.5px; color: var(--muted); }

  .steps { display: flex; flex-direction: column; gap: 5px; }
  .step { display: flex; align-items: baseline; gap: var(--s-2); font-size: 11.5px; }
  .step .title { color: var(--text-2); }
  .step .reason { color: var(--muted); }
</style>
