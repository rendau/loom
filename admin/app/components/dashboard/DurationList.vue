<script setup lang="ts">
import type { DashboardDagDuration } from '~/types/dashboard'

// Длительности ранов по дагам: полоска среднего времени относительно
// самого долгого дага + максимум подписью.

const props = defineProps<{ items: DashboardDagDuration[] }>()

const maxAvg = computed(() => Math.max(1, ...props.items.map(i => i.avg_sec)))

function formatSec(sec: number): string {
  const total = Math.round(sec)
  if (total < 60)
    return `${total}с`
  if (total < 3600)
    return `${Math.floor(total / 60)}м ${total % 60}с`
  return `${Math.floor(total / 3600)}ч ${Math.floor((total % 3600) / 60)}м`
}
</script>

<template>
  <div class="space-y-2">
    <div v-for="item in items" :key="dagRefLabel(runDagRef(item))" class="space-y-1">
      <div class="flex items-baseline justify-between gap-2 text-sm">
        <NuxtLink :to="dagLink(runDagRef(item))" class="truncate hover:text-primary hover:underline">
          {{ item.dag_name }}
        </NuxtLink>
        <span class="shrink-0 text-xs text-muted">
          ср. {{ formatSec(item.avg_sec) }} · макс. {{ formatSec(item.max_sec) }} · {{ item.runs }} ранов
        </span>
      </div>
      <div class="h-1.5 w-full overflow-hidden rounded-full bg-elevated">
        <div class="h-full rounded-full bg-primary" :style="{ width: `${Math.max(2, (item.avg_sec / maxAvg) * 100)}%` }" />
      </div>
    </div>
  </div>
</template>
