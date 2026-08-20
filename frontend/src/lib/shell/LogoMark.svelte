<script lang="ts">
  // Знак MitM. Разметка продублирована из frontend/src/assets/logo-mark.svg
  // осознанно: тот файл — исходник для растеризации иконок exe и трея, а здесь
  // нужен живой SVG, у которого можно анимировать потоки и подсветить части.
  // Импорт через ?raw + {@html} этого не дал бы — scoped-стили Svelte внутрь
  // такой вставки не попадают.

  /** Ширина знака в пикселях (высота считается по пропорции 144×128). */
  export let size: number = 128;
  /** compact — только ладонь с буквой M, без потоков (для шапки окна). */
  export let compact: boolean = false;
  /** flow — гнать «трафик» по линиям (заставка). */
  export let flow: boolean = false;

  // Градиенты в SVG адресуются по id, а знак на заставке и в шапке живёт на
  // странице одновременно — с одинаковыми id второй экземпляр забрал бы чужие.
  const uid = "mk" + Math.random().toString(36).slice(2, 8);
</script>

<svg
  width={size}
  height={(size * (compact ? 96 : 128)) / (compact ? 96 : 144)}
  viewBox={compact ? "0 0 96 96" : "0 0 144 128"}
  class:flow
  role="img"
  aria-label="MitM"
>
  <defs>
    <linearGradient id="{uid}-hand" gradientUnits="userSpaceOnUse" x1="20" y1="24" x2="91" y2="106">
      <stop offset="0" stop-color="#C4B5FD" />
      <stop offset="1" stop-color="#7C4DEF" />
    </linearGradient>
    <linearGradient id="{uid}-in" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="30" y2="0">
      <stop offset="0" stop-color="#FB7185" />
      <stop offset="1" stop-color="#FBBF24" />
    </linearGradient>
    <linearGradient id="{uid}-out" gradientUnits="userSpaceOnUse" x1="96" y1="0" x2="122" y2="0">
      <stop offset="0" stop-color="#A78BFA" />
      <stop offset="1" stop-color="#C4B5FD" />
    </linearGradient>
  </defs>

  {#if !compact}
    <g fill="none" stroke="url(#{uid}-in)" stroke-width="3" stroke-linecap="round">
      <path class="wire" d="M13 62h14" />
      <path class="wire" d="M10 78h17" />
      <path class="wire" d="M13 94h14" />
    </g>
    <g fill="url(#{uid}-in)">
      <circle cx="8" cy="62" r="3.2" />
      <circle cx="5" cy="78" r="3.2" />
      <circle cx="8" cy="94" r="3.2" />
    </g>
  {/if}

  <g transform={compact ? "translate(-6.17 -15.4) scale(0.976)" : "translate(12 0)"}>
    <!-- Большой палец лежит ПОВЕРХ ладони: под ней между фигурами оставался
         зазор в доли пикселя, а сверху шва не видно — общий градиент в
         userSpaceOnUse даёт обеим одинаковый цвет в точке стыка. -->
    <g fill="url(#{uid}-hand)">
      <rect x="40" y="36" width="10" height="46" rx="5" opacity=".78" />
      <rect x="53" y="24" width="10" height="58" rx="5" />
      <rect x="66" y="27" width="10" height="55" rx="5" opacity=".9" />
      <rect x="79" y="38" width="10" height="44" rx="5" opacity=".72" />
      <path d="M38 62h53v24a20 20 0 0 1-20 20H58a20 20 0 0 1-20-20z" />
      <path
        d="M50 94L27 80"
        fill="none"
        stroke="url(#{uid}-hand)"
        stroke-width="14"
        stroke-linecap="round"
      />
    </g>

    <g fill="none" stroke="#F5F3FF" stroke-width="7" stroke-linecap="round" stroke-linejoin="round">
      <path d="M51 97V73l13 17 13-17v24" />
    </g>

    {#if !compact}
      <g
        fill="none"
        stroke="url(#{uid}-out)"
        stroke-width="3"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <path class="wire out" d="M96 62h16" /><path d="M107 57l5 5-5 5" />
        <path class="wire out" d="M96 78h22" /><path d="M113 73l5 5-5 5" />
        <path class="wire out" d="M96 94h16" /><path d="M107 89l5 5-5 5" />
      </g>
    {/if}
  </g>
</svg>

<style>
  svg {
    display: block;
    overflow: visible;
  }

  /* Поток «течёт» пунктиром слева направо: знак на заставке живой, а не картинка.
     Дробим только сами провода — головки стрелок оставляем сплошными. */
  svg.flow .wire {
    stroke-dasharray: 5 7;
    animation: flow 1.1s linear infinite;
  }
  svg.flow .wire.out {
    animation-delay: 0.25s;
  }

  @keyframes flow {
    to {
      stroke-dashoffset: -12;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    svg.flow .wire {
      animation: none;
      stroke-dasharray: none;
    }
  }
</style>
