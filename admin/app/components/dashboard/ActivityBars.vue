<script setup lang="ts">
import type { ActivityPoint } from '~/utils/activity'

// Столбики «раны по времени»: успех снизу, провалы, сверху выполняющиеся.
// Точки готовит activityWindow — это могут быть и дни, и часы. Вёрстка на
// flex, а не SVG: столбики тянутся на всю ширину карточки, иначе у
// инсталляции с парой дагов график занимал бы левую четверть.

const props = defineProps<{ points: ActivityPoint[] }>()

const HEIGHT_PX = 120

const bars = computed(() => {
  const max = Math.max(1, ...props.points.map(p => p.success + p.failed + p.running))

  return props.points.map(p => ({
    ...p,
    total: p.success + p.failed + p.running,
    // доля высоты в процентах — столбик масштабируется вместе с колонкой
    successH: (p.success / max) * 100,
    failedH: (p.failed / max) * 100,
    runningH: (p.running / max) * 100,
  }))
})
</script>

<template>
  <div>
    <div class="flex items-end gap-1.5" :style="{ height: `${HEIGHT_PX}px` }">
      <div
        v-for="bar in bars"
        :key="bar.key"
        class="flex h-full max-w-14 min-w-2 flex-1 flex-col justify-end"
        :title="bar.title"
      >
        <div v-if="bar.runningH" class="rounded-t-sm bg-info" :style="{ height: `${bar.runningH}%` }" />
        <div v-if="bar.failedH" class="bg-error" :class="{ 'rounded-t-sm': !bar.runningH }" :style="{ height: `${bar.failedH}%` }" />
        <div v-if="bar.successH" class="bg-success" :class="{ 'rounded-t-sm': !bar.runningH && !bar.failedH }" :style="{ height: `${bar.successH}%` }" />
        <!-- интервал без ранов: тонкая риска, чтобы шкала не «проваливалась» -->
        <div v-if="!bar.total" class="h-0.5 rounded-sm bg-accented" />
      </div>
    </div>

    <div class="mt-1 flex items-end gap-1.5">
      <div v-for="bar in bars" :key="bar.key" class="max-w-14 min-w-2 flex-1 text-center text-xs text-dimmed">
        {{ bar.label }}
      </div>
    </div>

    <div class="mt-2 flex items-center gap-3 text-xs text-muted">
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-success" />успех</span>
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-error" />провал</span>
      <span class="flex items-center gap-1"><span class="size-2 rounded-sm bg-info" />выполняется</span>
    </div>
  </div>
</template>
