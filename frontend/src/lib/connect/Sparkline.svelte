<script lang="ts">
  // Мини-график по буферу из store. Голый inline-SVG: библиотека графиков ради
  // одной ломаной весила бы больше всего остального фронтенда.
  export let data: number[] = [];
  export let color: string = "var(--accent)";
  export let height: number = 34;

  const W = 100; // рисуем в условных координатах, растягивая по ширине карточки

  // Максимум берём по видимому окну, а не глобальный: иначе после одного пика
  // график навсегда прижимается к нулю и перестаёт что-либо показывать.
  $: max = Math.max(1, ...data);
  $: pts = data.map((v, i) => {
    const x = data.length < 2 ? 0 : (i / (data.length - 1)) * W;
    const y = height - (v / max) * (height - 2) - 1;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  });
  $: line = pts.join(" ");
  // Заливка под линией — та же ломаная, замкнутая по нижней кромке.
  $: area = pts.length ? `0,${height} ${line} ${W},${height}` : "";

  const uid = "sp" + Math.random().toString(36).slice(2, 8);
</script>

<svg viewBox="0 0 {W} {height}" preserveAspectRatio="none" style="height:{height}px">
  <defs>
    <linearGradient id={uid} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color={color} stop-opacity="0.4" />
      <stop offset="1" stop-color={color} stop-opacity="0" />
    </linearGradient>
  </defs>
  {#if pts.length > 1}
    <polygon points={area} fill="url(#{uid})" />
    <polyline
      points={line}
      fill="none"
      stroke={color}
      stroke-width="1.6"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
  {/if}
</svg>

<style>
  svg {
    display: block;
    width: 100%;
    overflow: visible;
  }
</style>
