<script setup lang="ts">
import type { BreadcrumbItem } from '@nuxt/ui'

// Шапка карточки: путь от раздела до текущей сущности плюс её тип.
// Идентификаторы у нас составные и похожие друг на друга («demo/nsi_sync»
// — и шаблон, и даг), поэтому одного заголовка мало: без типа непонятно,
// на что смотришь.

defineProps<{
  items: BreadcrumbItem[]
  // «проект», «даг в образе», «даг», «ран» — что за сущность открыта
  kind: string
}>()
</script>

<template>
  <div class="flex min-w-0 items-center gap-2">
    <UBreadcrumb
      :items="items"
      :ui="{
        root: 'min-w-0',
        list: 'flex-nowrap',
        item: 'min-w-0',
        link: 'min-w-0 truncate',
        linkLeadingIcon: 'shrink-0',
      }"
    >
      <!-- последний элемент — не ссылка, а текущее место: он должен
           читаться как заголовок, а не как ещё один переход -->
      <template #item-label="{ item, index }">
        <span :class="index === items.length - 1 ? 'font-semibold text-highlighted' : ''">{{ item.label }}</span>
      </template>
    </UBreadcrumb>
    <UBadge color="neutral" variant="subtle" size="sm" class="shrink-0">{{ kind }}</UBadge>
  </div>
</template>
