<script setup lang="ts">
import type { Run } from '~/types/run'

// Мини-бар длительностей последних ранов дага: столбик — ран (старые
// слева), высота — длительность относительно максимума, цвет — статус.
// Тот же рукописный SVG-приём, что в DashboardActivityBars.

const props = defineProps<{ runs: Run[] }>()

const BAR_W = 10
const GAP = 3
const HEIGHT = 44

const layout = computed(() => {
  // хронологически: старые слева
  const runs = [...props.runs].reverse()
  const items = runs.map((r) => {
    const sec = r.finished_at
      ? Math.max(0, (new Date(r.finished_at).getTime() - new Date(r.created_at).getTime()) / 1000)
      : 0
    return { run: r, sec }
  })
  const max = Math.max(1, ...items.map(i => i.sec))
  return {
    bars: items.map((i, idx) => ({
      ...i,
      x: idx * (BAR_W + GAP),
      h: Math.max(3, (i.sec / max) * (HEIGHT - 4)),
    })),
    width: Math.max(1, items.length) * (BAR_W + GAP) - GAP,
  }
})

function barColor(status: string): string {
  switch (status) {
    case 'success': return 'var(--ui-success)'
    case 'failed': return 'var(--ui-error)'
    case 'running': return 'var(--ui-info)'
    default: return 'var(--ui-border-accented)'
  }
}
</script>

<template>
  <svg :width="layout.width" :height="HEIGHT" class="block shrink-0">
    <g v-for="bar in layout.bars" :key="bar.run.id">
      <title>{{ bar.run.id }}: {{ runStatusLabel(bar.run.status) }}, {{ formatDuration(bar.run.created_at, bar.run.finished_at) }}</title>
      <rect
        :x="bar.x"
        :y="HEIGHT - bar.h"
        :width="BAR_W"
        :height="bar.h"
        rx="1.5"
        :fill="barColor(bar.run.status)"
        :opacity="bar.run.status === 'running' ? 0.6 : 1"
      />
    </g>
  </svg>
</template>
