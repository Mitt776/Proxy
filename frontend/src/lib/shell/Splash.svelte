<script lang="ts">
  // Заставка при запуске: знак во весь экран → уезжает в свой слот в шапке.
  //
  // Ничего не блокирует. Подписки на события и загрузка данных идут под ней, так
  // что двух секунд к старту она не добавляет — просто закрывает собой время,
  // которое и так уходит на инициализацию WebView2 и первый GetAppInfo.
  import { createEventDispatcher, onDestroy, onMount, tick } from "svelte";
  import LogoMark from "./LogoMark.svelte";
  import { t } from "../i18n";

  /** Элемент-слот в шапке окна, куда должен приземлиться знак. */
  export let target: HTMLElement | null = null;

  const dispatch = createEventDispatcher<{ landed: void; done: void }>();

  const MARK_SIZE = 220; // ширина знака на заставке
  const HOLD = 1200; // сколько держим композицию, мс
  const FLY = 620; // длительность перелёта, мс

  let markEl: HTMLElement;
  let phase: "in" | "fly" | "gone" = "in";
  let transform = "";
  let reduced = false;

  const timers: ReturnType<typeof setTimeout>[] = [];
  const later = (fn: () => void, ms: number) => timers.push(setTimeout(fn, ms));

  onMount(() => {
    reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    later(fly, reduced ? 700 : HOLD);
  });

  onDestroy(() => timers.forEach(clearTimeout));

  async function fly() {
    // Приём FLIP: цель уже отрисована (хоть и прозрачна), поэтому её настоящие
    // координаты известны — считаем их и анимируем transform. Иначе знак
    // приземлялся бы «примерно туда», а на другом масштабе экрана — мимо.
    if (target && markEl && !reduced) {
      const from = markEl.getBoundingClientRect();
      const to = target.getBoundingClientRect();
      const scale = to.width / from.width;
      const dx = to.left + to.width / 2 - (from.left + from.width / 2);
      const dy = to.top + to.height / 2 - (from.top + from.height / 2);
      transform = `translate(${dx}px, ${dy}px) scale(${scale})`;
    }
    phase = "fly";
    await tick();

    // Знак в шапке показываем ровно в момент прибытия — так подмены не видно.
    later(() => dispatch("landed"), reduced ? 200 : FLY - 40);
    later(() => {
      phase = "gone";
      dispatch("done");
    }, reduced ? 300 : FLY);
  }
</script>

{#if phase !== "gone"}
  <div class="splash" class:fading={phase === "fly"} class:reduced>
    <div class="glow" />
    <div class="stack" class:fly={phase === "fly"}>
      <div
        class="mark"
        bind:this={markEl}
        style="transform:{transform}; transition-duration:{FLY}ms"
      >
        <LogoMark size={MARK_SIZE} flow />
      </div>
      <div class="word">MITM</div>
      <div class="tagline">{$t("app.tagline")}</div>
    </div>
  </div>
{/if}

<style>
  .splash {
    position: fixed;
    inset: 0;
    z-index: 500;
    background: var(--bg);
    display: flex;
    align-items: center;
    justify-content: center;
    animation: appear 0.4s ease both;
  }
  /* Гасим не сам знак, а подложку: знак в это время уже летит в шапку и должен
     остаться видимым до самого приземления. */
  .splash.fading {
    animation: dim 0.55s ease forwards;
  }

  .glow {
    position: absolute;
    width: 620px;
    height: 620px;
    border-radius: 50%;
    background: radial-gradient(circle, rgba(139, 92, 246, 0.28), transparent 62%);
    pointer-events: none;
  }

  .stack {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--s-4);
  }

  .mark {
    transition-property: transform;
    transition-timing-function: cubic-bezier(0.55, 0, 0.2, 1);
    filter: drop-shadow(0 0 30px rgba(139, 92, 246, 0.45));
    will-change: transform;
  }

  .word {
    font-size: 46px;
    font-weight: 800;
    letter-spacing: 0.14em;
    background: linear-gradient(90deg, #C4B5FD, #EDE9FE 50%, #A78BFA);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
    transition: opacity 0.3s, transform 0.3s;
  }
  .tagline {
    font-size: 12px;
    letter-spacing: 0.34em;
    color: var(--muted);
    transition: opacity 0.3s, transform 0.3s;
  }

  /* Надпись и слоган растворяются раньше знака — он один продолжает движение. */
  .stack.fly .word,
  .stack.fly .tagline {
    opacity: 0;
    transform: translateY(8px);
  }

  @keyframes appear {
    from {
      opacity: 0;
    }
  }
  @keyframes dim {
    to {
      background: transparent;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .splash,
    .splash.fading {
      animation: none;
    }
    .splash.reduced {
      transition: opacity 0.3s;
    }
    .stack.fly {
      opacity: 0;
      transition: opacity 0.3s;
    }
  }
</style>
