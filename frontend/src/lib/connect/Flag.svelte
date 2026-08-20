<script lang="ts" context="module">
  // Круглые флаги из набора circle-flags (MIT), вшиты в assets/flags.
  //
  // Эмодзи-флаги на Windows не работают: в Segoe UI Emoji нет глифов для пар
  // региональных индикаторов, и вместо флага рисуются две буквы — так и было в
  // прежней панели подключения.
  //
  // eager+raw: Vite подставляет разметку прямо в бандл. Ленивый импорт дал бы
  // мигание пустого круга на каждом переподключении, а весь набор — 265 файлов
  // и меньше 200 КБ текста, который прекрасно жмётся.
  const files = import.meta.glob("../../assets/flags/*.svg", {
    eager: true,
    as: "raw",
  }) as Record<string, string>;

  const byCode: Record<string, string> = {};
  for (const path in files) {
    const code = path.slice(path.lastIndexOf("/") + 1, -4);
    byCode[code] = files[path];
  }

  /** hasFlag — есть ли в наборе флаг для этого кода страны. */
  export function hasFlag(cc: string): boolean {
    return !!cc && cc.toLowerCase() in byCode;
  }
</script>

<script lang="ts">
  import Icon from "../icons/Icon.svelte";

  /** Двухбуквенный код страны (ISO 3166-1 alpha-2). Пусто — серый глобус. */
  export let code: string = "";
  export let size: number = 96;

  $: svg = byCode[(code || "").toLowerCase()] ?? "";
</script>

<span class="flag" style="--d:{size}px" class:empty={!svg}>
  {#if svg}
    <!-- Растягиваем только вставленный флаг: общий селектор svg дотягивался бы
         и до иконки-глобуса, у которой свои размеры, и превращал её в овал. -->
    <span class="pic">{@html svg}</span>
  {:else}
    <Icon name="globe" size={size * 0.5} stroke={1.2} />
  {/if}
</span>

<style>
  .flag {
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    width: var(--d);
    height: var(--d);
    min-width: var(--d);
    aspect-ratio: 1;
    border-radius: 50%;
    overflow: hidden;
    flex: none;
    /* Кольцо и свечение общие для всех состояний; цвет берётся из --state,
       поэтому флаг сам гаснет при отключении вместе с остальным интерфейсом. */
    box-shadow: 0 0 0 3px var(--panel), 0 0 0 5px var(--state), 0 0 34px -6px var(--state);
    transition: box-shadow 0.3s;
  }
  .flag.empty {
    background: var(--panel-2);
    color: var(--muted);
  }
  .pic {
    display: block;
    width: 100%;
    height: 100%;
  }
  .pic :global(svg) {
    width: 100%;
    height: 100%;
    display: block;
  }
</style>
