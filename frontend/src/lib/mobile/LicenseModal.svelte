<script lang="ts">
  // Текст GPLv3 внутри приложения.
  //
  // Ссылки на исходники недостаточно: ядро sing-box линкуется в APK, а GPLv3
  // требует отдать получателю копию самой лицензии вместе с программой. Файл
  // едет в ассетах APK (Gradle-задача copyLicense) и отдаётся тем же
  // WebViewAssetLoader, что и интерфейс: страница лежит в /assets/web/, лицензия
  // рядом в /assets/ — отсюда «../LICENSE.txt».
  import { createEventDispatcher, onMount } from "svelte";
  import Icon from "../icons/Icon.svelte";
  import { t } from "../i18n";

  const dispatch = createEventDispatcher<{ close: void }>();

  let text = "";
  let failed = false;

  onMount(async () => {
    try {
      const res = await fetch("../LICENSE.txt");
      if (!res.ok) throw new Error(String(res.status));
      text = await res.text();
    } catch (e) {
      failed = true;
    }
  });
</script>

<div class="modal-backdrop" role="button" tabindex="0"
     on:click={() => dispatch("close")}
     on:keydown={(e) => e.key === "Escape" && dispatch("close")}>
  <div class="modal" role="dialog" on:click|stopPropagation on:keydown|stopPropagation>
    <div class="modal-h">
      GNU GPL v3
      <button class="icon-btn" on:click={() => dispatch("close")} aria-label={$t("common.close")}>
        <Icon name="close" size={15} />
      </button>
    </div>

    <div class="modal-b">
      {#if failed}
        <div class="hint">{$t("m.settings.licenseFail")}</div>
      {:else}
        <pre>{text}</pre>
      {/if}
    </div>
  </div>
</div>

<style>
  /* Лицензия — простыня в 674 строки: во весь экран и моноширинным, как её и
     печатают. Перенос по словам обязателен — у текста FSF строки до 79 знаков,
     а на 360 px это горизонтальная прокрутка на каждой. */
  .modal { width: 100%; height: 100%; max-width: none; max-height: none; border-radius: 0; }
  pre {
    margin: 0;
    font-family: ui-monospace, Consolas, monospace;
    font-size: 11px;
    line-height: 1.5;
    color: var(--text-2);
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
