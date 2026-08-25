<script setup lang="ts">
import type { DropdownMenuItem } from '@nuxt/ui'

// «⋯»-меню действий строки/карточки (design/05: ≤2 частых действий
// inline, остальное — сюда; разрушительные — отдельной группой внизу).

defineProps<{
  items: DropdownMenuItem[][]
  size?: 'sm' | 'md'
}>()
</script>

<template>
  <UDropdownMenu :items="items" :content="{ align: 'end' }">
    <UButton
      icon="i-lucide-ellipsis-vertical"
      :size="size ?? 'sm'"
      color="neutral"
      variant="ghost"
      aria-label="Действия"
    />
  </UDropdownMenu>
</template>
