<script lang="ts">
  // Круглая кнопка включения. Объём собран слоями теней, без картинок:
  // утопленное кольцо-шайба, на нём выпуклая шапка с верхним бликом и жёсткой
  // нижней тенью. При нажатии шапка уезжает вниз, тень схлопывается, а блик
  // слабеет — глаз читает это как реальное продавливание.
  import Icon from "../icons/Icon.svelte";

  /** Состояние ядра: stopped | starting | running | error. */
  export let state: string = "stopped";
  export let disabled: boolean = false;
  export let size: number = 200;
  /** Подпись под иконкой (Подключить / Отключить). */
  export let label: string = "";

  $: on = state === "running";
  $: busy = state === "starting";
</script>

<button
  class="power"
  class:on
  class:busy
  class:err={state === "error"}
  style="--size:{size}px"
  {disabled}
  on:click
>
  <span class="ring">
    <span class="cap">
      <Icon name="power" size={size * 0.26} stroke={2.2} />
      {#if label}<span class="label">{label}</span>{/if}
    </span>
  </span>
</button>

<style>
  .power {
    width: var(--size);
    height: var(--size);
    padding: 0;
    border: none;
    background: none;
    border-radius: 50%;
    cursor: pointer;
    display: block;
  }
  .power:disabled {
    cursor: default;
    opacity: 0.5;
  }

  /* Шайба: утопленное кольцо, в котором сидит кнопка. */
  .ring {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    border-radius: 50%;
    background: linear-gradient(160deg, var(--panel), var(--bg));
    box-shadow:
      inset 0 3px 8px rgba(0, 0, 0, 0.75),
      inset 0 -2px 4px rgba(255, 255, 255, 0.05),
      0 0 0 1px var(--line);
    transition: box-shadow 0.3s;
  }

  /* Шапка кнопки — то, что реально «нажимается». */
  .cap {
    position: relative;
    width: 78%;
    height: 78%;
    border-radius: 50%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--text-2);
    background: linear-gradient(180deg, var(--line-2), var(--panel-2) 62%, var(--panel));
    box-shadow:
      inset 0 2px 1px rgba(255, 255, 255, 0.18),
      inset 0 -10px 18px -10px rgba(0, 0, 0, 0.65),
      0 8px 0 -2px rgba(0, 0, 0, 0.55),
      0 14px 22px -6px rgba(0, 0, 0, 0.7);
    transform: translateY(-3px);
    transition:
      transform 0.09s cubic-bezier(0.3, 0.8, 0.4, 1),
      box-shadow 0.09s,
      background 0.3s,
      color 0.3s;
  }

  /* Верхний блик отдельным слоем: без него градиент читается плоско. */
  .cap::before {
    content: "";
    position: absolute;
    inset: 6% 12% 46%;
    border-radius: 50%;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.18), transparent);
    pointer-events: none;
  }

  .label {
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  /* --- Состояния --- */

  .power.on .cap {
    color: #fff;
    background: linear-gradient(180deg, var(--accent-2), var(--accent) 58%, #6D3EE0);
    box-shadow:
      inset 0 2px 1px rgba(255, 255, 255, 0.32),
      inset 0 -10px 18px -10px rgba(0, 0, 0, 0.5),
      0 8px 0 -2px #4B2AA0,
      0 14px 26px -4px rgba(139, 92, 246, 0.55);
  }
  .power.on .ring {
    box-shadow:
      inset 0 3px 8px rgba(0, 0, 0, 0.75),
      inset 0 -2px 4px rgba(255, 255, 255, 0.05),
      0 0 0 1px var(--accent),
      var(--glow);
  }

  .power.err .cap {
    color: #fff;
    background: linear-gradient(180deg, #FDA4AF, var(--danger) 58%, #C4485C);
    box-shadow:
      inset 0 2px 1px rgba(255, 255, 255, 0.3),
      0 8px 0 -2px #8F3242,
      0 14px 26px -4px rgba(251, 113, 133, 0.45);
  }

  .power.busy .cap {
    color: var(--warn);
    animation: pulse 1.1s ease-in-out infinite;
  }

  /* --- Нажатие --- */

  .power:active:not(:disabled) .cap {
    transform: translateY(3px);
    box-shadow:
      inset 0 3px 10px rgba(0, 0, 0, 0.55),
      inset 0 1px 1px rgba(255, 255, 255, 0.1),
      0 1px 0 -1px rgba(0, 0, 0, 0.5),
      0 3px 8px -4px rgba(0, 0, 0, 0.6);
  }
  .power:active:not(:disabled) .cap::before {
    opacity: 0.35;
  }

  .power:focus-visible .ring {
    box-shadow:
      inset 0 3px 8px rgba(0, 0, 0, 0.75),
      0 0 0 3px var(--accent-dim),
      0 0 0 1px var(--accent);
  }

  @keyframes pulse {
    50% {
      box-shadow:
        inset 0 2px 1px rgba(255, 255, 255, 0.18),
        0 8px 0 -2px rgba(0, 0, 0, 0.55),
        0 14px 30px -4px rgba(251, 191, 36, 0.45);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .power.busy .cap {
      animation: none;
    }
  }
</style>
