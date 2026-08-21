<script lang="ts">
  // Заглушка раздела, до которого порт ещё не дошёл (этапы 3 и 4). Держим её
  // честной и видимой, а не прячем пункт из меню: список разделов на телефоне
  // и на ПК должен совпадать, иначе непонятно, чего в сборке не хватает.
  import Icon from "../icons/Icon.svelte";
  import type { paths } from "../icons/paths";
  import { t } from "../i18n";

  export let icon: keyof typeof paths;
  export let title: string;
</script>

<div class="soon">
  <Icon name={icon} size={34} stroke={1.3} />
  <h2>{title}</h2>
  <p>{$t("m.soon")}</p>
</div>

<style>
  .soon {
    min-height: 100%;
    /* Иначе padding прибавляется к 100% и страница получает лишние 48 пикселей
       высоты — с полосой прокрутки в пустом разделе. */
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--s-3);
    padding: var(--s-5);
    color: var(--muted);
    text-align: center;
  }
  h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 700;
    color: var(--text-2);
  }
  p {
    margin: 0;
    font-size: 13px;
  }
</style>
