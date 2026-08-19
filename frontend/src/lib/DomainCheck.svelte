<script lang="ts">
  // Проверка домена до подключения: куда он уйдёт по текущим правилам.
  // Считает бэкенд, спрашивая само ядро (`sing-box rule-set match`), поэтому
  // ответ совпадает с боевым поведением, а не с нашими догадками.
  import { CheckDomain } from "../../wailsjs/go/main/App";
  import type { config } from "../../wailsjs/go/models";

  let domain = "";
  let busy = false;
  let error = "";
  let result: config.DomainCheck | null = null;
  let details = false;

  const actionLabels: Record<string, string> = {
    proxy: "через прокси", direct: "напрямую", block: "заблокирован",
  };

  async function check() {
    if (!domain.trim() || busy) return;
    busy = true;
    error = "";
    try {
      result = await CheckDomain(domain);
      details = false;
    } catch (e) {
      result = null;
      error = String(e);
    } finally {
      busy = false;
    }
  }

  // Показываем только то, что пользователю действительно стоит увидеть:
  // сработавшее правило и всё, что проверить не удалось.
  $: notable = (result?.steps || []).filter((s) => s.status !== "miss");
</script>

<div class="check-box">
  <div class="row">
    <input class="fld" placeholder="Проверить домен: youtube.com" bind:value={domain}
           on:keydown={(e) => e.key === "Enter" && check()} />
    <button class="mini wide" on:click={check} disabled={busy || !domain.trim()}>
      {busy ? "…" : "Проверить"}
    </button>
  </div>

  {#if error}
    <div class="error">{error}</div>
  {:else if result}
    <div class="verdict">
      <span class="dom">{result.domain}</span>
      <span class="arrow">→</span>
      <span class="act {result.action}">{actionLabels[result.action] || result.action}</span>
      {#if result.target}<span class="via">через «{result.target}»</span>{/if}
      <span class="why">
        {#if result.mode !== "Rule"}
          режим «{result.mode === "Global" ? "всё через прокси" : "всё напрямую"}» перекрывает правила
        {:else if result.byFinal}
          ни одно правило не подошло — сработало «остальной трафик»
        {:else}
          правило «{result.ruleTitle}»
        {/if}
      </span>
      {#if notable.length > 1 || (notable.length === 1 && notable[0].status !== "match")}
        <button class="mini" title="Подробности" on:click={() => (details = !details)}>
          {details ? "▲" : "▼"}
        </button>
      {/if}
    </div>

    {#if details}
      <div class="steps">
        {#each notable as s}
          <div class="step {s.status}">
            <span class="badge">
              {s.status === "match" ? "сработало" : s.status === "unknown" ? "не проверить" : "пропущено"}
            </span>
            <span class="title">{s.title}</span>
            {#if s.reason}<span class="reason">{s.reason}</span>{/if}
          </div>
        {/each}
        <div class="hint">
          Проверка знает только домен: правила по IP, порту и процессу вживую могут
          сработать раньше.
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .check-box {
    display: flex; flex-direction: column; gap: 6px; background: var(--bg);
    border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px;
  }
  .row { display: flex; gap: 6px; }
  .row .fld { flex: 1; min-width: 0; }

  .verdict {
    display: flex; align-items: center; gap: 7px; flex-wrap: wrap;
    font-size: 12.5px; text-align: left;
  }
  .dom { font-weight: 700; }
  .arrow { color: var(--muted-2); }
  .act { font-weight: 800; }
  .act.proxy { color: #58a6ff; }
  .act.direct { color: var(--green); }
  .act.block { color: var(--red); }
  .via { color: var(--text-2); }
  .why { color: var(--muted); font-size: 11.5px; margin-left: auto; }

  .steps { display: flex; flex-direction: column; gap: 4px; text-align: left; }
  .step { display: flex; align-items: baseline; gap: 7px; font-size: 11.5px; }
  .badge {
    flex: none; font-size: 10px; font-weight: 700; padding: 1px 7px;
    border-radius: 999px; background: var(--line); color: var(--muted);
  }
  .step.match .badge { background: #14351f; color: var(--green); }
  .step.unknown .badge { background: #3a2d00; color: var(--yellow); }
  .step .title { color: var(--text-2); }
  .step .reason { color: var(--muted-2); }
</style>
