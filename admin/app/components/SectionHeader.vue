<script setup lang="ts">
// Заголовок секции страницы: компактный (text-sm), опциональный счётчик
// и слот действий справа.
defineProps<{
  title: string
  count?: number
}>()
</script>

<template>
  <div class="mb-2 flex items-center justify-between gap-2">
    <h3 class="text-sm font-semibold text-highlighted">
      {{ title }}<span v-if="count !== undefined" class="ms-1.5 font-normal text-muted">{{ count }}</span>
    </h3>
    <slot />
  </div>
</template>
