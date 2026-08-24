<script setup lang="ts">
// Карточка-счётчик дашборда: крупное значение, подпись и опциональная
// расшифровка (например «12 успешно · 1 провал»).
defineProps<{
  label: string
  value: string | number
  icon: string
  color?: 'primary' | 'success' | 'warning' | 'error' | 'info' | 'neutral'
  hint?: string
  to?: string
}>()
</script>

<template>
  <component :is="to ? 'NuxtLink' : 'div'" :to="to" class="block">
    <UCard :ui="{ body: 'p-4 sm:p-4' }" :class="to ? 'transition hover:border-primary' : ''">
      <div class="flex items-start gap-3">
        <UIcon :name="icon" class="mt-0.5 size-5 shrink-0" :class="`text-${color ?? 'primary'}`" />
        <div class="min-w-0">
          <div class="text-2xl font-semibold text-highlighted">{{ value }}</div>
          <div class="text-sm text-muted">{{ label }}</div>
          <div v-if="hint" class="mt-0.5 text-xs text-dimmed">{{ hint }}</div>
        </div>
      </div>
    </UCard>
  </component>
</template>
