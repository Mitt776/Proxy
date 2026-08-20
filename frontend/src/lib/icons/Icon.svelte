<script lang="ts">
  // Единственный компонент-иконка на всё приложение: путь берётся из таблицы
  // paths.ts по имени. Так набор не тянет за собой ни библиотеку, ни два десятка
  // мелких .svelte-файлов, а размер и цвет задаются снаружи — цвет всегда
  // currentColor, поэтому иконка сама подхватывает цвет текста рядом.
  import { paths, filled } from "./paths";

  export let name: keyof typeof paths;
  export let size: number = 18;
  /** Толщина обводки. Для мелких размеров имеет смысл поднимать до 2. */
  export let stroke: number = 1.6;
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={filled.has(name) ? "currentColor" : "none"}
  stroke={filled.has(name) ? "none" : "currentColor"}
  stroke-width={stroke}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {@html paths[name]}
</svg>

<style>
  svg {
    display: block;
    flex: none;
  }
</style>
