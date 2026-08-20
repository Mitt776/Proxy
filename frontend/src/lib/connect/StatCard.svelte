<script lang="ts">
  // Компактная карточка показателя: строка «иконка + подпись … значение», под
  // ней график во всю ширину. Итог за сессию не занимает отдельной строки —
  // он уезжает в подсказку, иначе колонка карточек перестаёт быть миниатюрной.
  import Icon from "../icons/Icon.svelte";
  import type { paths } from "../icons/paths";
  import Sparkline from "./Sparkline.svelte";

  export let icon: keyof typeof paths;
  export let label: string;
  export let value: string;
  export let sub: string = "";
  export let color: string = "var(--accent)";
  export let data: number[] = [];
</script>

<div class="card stat" title={sub}>
  <div class="head">
    <span class="ico" style="color:{color}"><Icon name={icon} size={14} /></span>
    <span class="label">{label}</span>
    <span class="value tnum">{value}</span>
  </div>
  <Sparkline {data} {color} height={28} />
</div>

<style>
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    border-radius: var(--r-3);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .ico {
    display: flex;
  }
  .label {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .value {
    font-size: 12px;
    font-weight: 700;
    color: var(--text);
    flex: none;
  }
</style>
