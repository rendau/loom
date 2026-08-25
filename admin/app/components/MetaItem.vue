<script setup lang="ts">
// Элемент MetaGrid: подпись + значение (слот). span — на две колонки
// (длинные значения: образ, digest).
defineProps<{
  label: string
  span?: boolean
}>()
</script>

<template>
  <div :class="span ? 'col-span-2' : ''" class="min-w-0">
    <div class="text-xs text-muted">{{ label }}</div>
    <div class="min-w-0">
      <slot>
        <span class="text-muted">—</span>
      </slot>
    </div>
  </div>
</template>
