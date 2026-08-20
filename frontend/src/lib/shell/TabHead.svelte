<script lang="ts">
  // Шапка раздела: заголовок с подписью слева, действия — справа. Отдельным
  // компонентом, чтобы пять вкладок не расходились по кеглю и отступам.
  export let title: string;
  export let sub: string = "";
</script>

<div class="tab-head">
  <div>
    <div class="ttl">{title}</div>
    {#if sub}<div class="sub">{sub}</div>{/if}
  </div>
  <div class="tools"><slot /></div>
</div>
