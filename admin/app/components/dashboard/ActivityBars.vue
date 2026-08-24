<script setup lang="ts">
import type { DashboardDay } from '~/types/dashboard'

// Столбики «раны по дням»: успех снизу, провалы сверху. Рукописный SVG —
// ради двух графиков chart-либа не нужна (тот же приём, что в RunDagGraph).

const props = defineProps<{ days: DashboardDay[] }>()

const BAR_W = 18
const GAP = 6
const HEIGHT = 120
const PAD_BOTTOM = 18

const layout = computed(() => {
  const days = props.days.map(d => ({
    date: d.date,
    success: Number(d.success),
    failed: Number(d.failed),
    running: Number(d.running),
  }))
  const max = Math.max(1, ...days.map(d => d.success + d.failed + d.running))
  const scale = (HEIGHT - PAD_BOTTOM - 4) / max

  const bars = days.map((d, i) => {
    const x = i * (BAR_W + GAP)
    const successH = d.success * scale
    const failedH = d.failed * scale
    const runningH = d.running * scale
    const base = HEIGHT - PAD_BOTTOM
    return {
      ...d,
      x,
      total: d.success + d.failed + d.running,
      // снизу вверх: успех → провалы → выполняются
      successY: base - successH,
      successH,
      failedY: base - successH - failedH,
      failedH,
      runningY: base - successH - failedH - runningH,
      runningH,
      label: d.date.slice(8), // день месяца
      base,
    }
  })

  return { bars, width: Math.max(1, days.length) * (BAR_W + GAP) - GAP, max }
})
</script>

<template>
  <div class="overflow-x-auto">
    <svg :width="layout.width" :height="HEIGHT" class="block">
      <g v-for="bar in layout.bars" :key="bar.date">
        <title>{{ bar.date }}: {{ bar.success }} успешно, {{ bar.failed }} провал, {{ bar.running }} выполняется</title>
        <rect
          v-if="bar.total === 0"
          :x="bar.x"
          :y="bar.base - 2"
          :width="BAR_W"
          height="2"
          rx="1"
          fill="var(--ui-border-accented)"
        />
        <rect v-if="bar.successH > 0" :x="bar.x" :y="bar.successY" :width="BAR_W" :height="bar.successH" rx="2" fill="var(--ui-success)" />
        <rect v-if="bar.failedH > 0" :x="bar.x" :y="bar.failedY" :width="BAR_W" :height="bar.failedH" rx="2" fill="var(--ui-error)" />
        <rect v-if="bar.runningH > 0" :x="bar.x" :y="bar.runningY" :width="BAR_W" :height="bar.runningH" rx="2" fill="var(--ui-info)" />
        <text
          :x="bar.x + BAR_W / 2"
          :y="HEIGHT - 4"
          text-anchor="middle"
          font-size="10"
          fill="var(--ui-text-dimmed)"
        >{{ bar.label }}</text>
      </g>
    </svg>

    <div class="mt-1 flex items-center gap-3 text-xs text-muted">
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-success" />успех</span>
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-error" />провал</span>
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-info" />выполняется</span>
    </div>
  </div>
</template>
