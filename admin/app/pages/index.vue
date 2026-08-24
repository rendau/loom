<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'
import { getDashboard } from '~/api/dashboard.api'
import type { Dashboard } from '~/types/dashboard'

// Дашборд: сводка по инсталляции — активность, расписания, пулы, провалы.

const data = ref<Dashboard | null>(null)
const loading = ref(false)
const loadError = ref('')

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    data.value = await getDashboard()
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    if (!background)
      loading.value = false
  }
}

let pollTimer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  pollTimer = setInterval(() => load(true), 30_000)
})
onBeforeUnmount(() => clearInterval(pollTimer))

function windowHint(w?: { success: string, failed: string }): string {
  if (!w)
    return ''
  return `${w.success} успешно · ${w.failed} провал`
}

function windowTotal(w?: { success: string, failed: string }): number {
  return w ? Number(w.success) + Number(w.failed) : 0
}

// доля успешных ранов за неделю — главный индикатор здоровья
const successRate7d = computed(() => {
  const total = windowTotal(data.value?.last_7d)
  if (!total)
    return '—'
  return `${Math.round((Number(data.value!.last_7d.success) / total) * 100)}%`
})
</script>

<template>
  <UDashboardPanel id="dashboard">
    <template #header>
      <UDashboardNavbar title="Дашборд">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load()" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" />

      <template v-if="data">
        <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <DashboardStatCard
            label="Активных ранов"
            :value="data.active_runs"
            icon="i-lucide-activity"
            color="info"
            to="/runs?status=running"
          />
          <DashboardStatCard
            label="Ранов за сутки"
            :value="windowTotal(data.last_24h)"
            :hint="windowHint(data.last_24h)"
            icon="i-lucide-calendar-days"
            color="primary"
          />
          <DashboardStatCard
            label="Успешных за неделю"
            :value="successRate7d"
            :hint="windowHint(data.last_7d)"
            icon="i-lucide-circle-check"
            color="success"
          />
          <DashboardStatCard
            label="Дагов"
            :value="data.dag_count"
            :hint="Number(data.paused_dag_count) > 0 ? `${data.paused_dag_count} на паузе` : 'все активны'"
            icon="i-lucide-workflow"
            color="neutral"
            to="/dags"
          />
        </div>

        <section>
          <h3 class="mb-2 font-semibold text-highlighted">Активность за 2 недели</h3>
          <UCard :ui="{ body: 'p-4 sm:p-4' }">
            <DashboardActivityBars :days="data.activity ?? []" />
          </UCard>
        </section>

        <div class="grid gap-4 lg:grid-cols-2">
          <section>
            <h3 class="mb-2 font-semibold text-highlighted">Ближайшие запуски</h3>
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="data.upcoming?.length" class="space-y-2">
                <div v-for="item in data.upcoming" :key="item.dag_name" class="flex items-baseline justify-between gap-2 text-sm">
                  <NuxtLink :to="`/dags/${encodeURIComponent(item.dag_name)}`" class="truncate hover:text-primary hover:underline">
                    {{ item.dag_name }}
                  </NuxtLink>
                  <span class="shrink-0 text-xs text-muted">
                    <span class="font-mono">{{ item.schedule }}</span> · {{ formatDateTime(item.next_run_at) }}
                  </span>
                </div>
              </div>
              <p v-else class="text-sm text-muted">Дагов с расписанием нет.</p>
            </UCard>
          </section>

          <section>
            <h3 class="mb-2 font-semibold text-highlighted">Пулы слотов</h3>
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="data.pools?.length" class="space-y-2">
                <div v-for="pool in data.pools" :key="pool.name" class="space-y-1">
                  <div class="flex items-baseline justify-between gap-2 text-sm">
                    <span class="truncate">{{ pool.name }}</span>
                    <span class="shrink-0 text-xs text-muted">{{ pool.busy }} / {{ pool.slots }}</span>
                  </div>
                  <div class="h-1.5 w-full overflow-hidden rounded-full bg-elevated">
                    <div
                      class="h-full rounded-full"
                      :class="Number(pool.busy) >= Number(pool.slots) ? 'bg-warning' : 'bg-primary'"
                      :style="{ width: `${Number(pool.slots) ? Math.min(100, (Number(pool.busy) / Number(pool.slots)) * 100) : 0}%` }"
                    />
                  </div>
                </div>
              </div>
              <p v-else class="text-sm text-muted">Пулов нет.</p>
            </UCard>
          </section>

          <section>
            <h3 class="mb-2 font-semibold text-highlighted">Последние провалы</h3>
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="data.recent_failures?.length" class="space-y-2">
                <div v-for="run in data.recent_failures" :key="run.run_id" class="flex items-baseline justify-between gap-2 text-sm">
                  <NuxtLink :to="`/runs/${encodeURIComponent(run.run_id)}`" class="truncate font-mono text-xs hover:text-primary hover:underline">
                    {{ run.run_id }}
                  </NuxtLink>
                  <span class="shrink-0 text-xs text-muted">{{ formatDateTime(run.finished_at) }}</span>
                </div>
              </div>
              <p v-else class="text-sm text-muted">Провалов нет.</p>
            </UCard>
          </section>

          <section>
            <h3 class="mb-2 font-semibold text-highlighted">Длительность ранов (неделя)</h3>
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <DashboardDurationList v-if="data.dag_durations?.length" :items="data.dag_durations" />
              <p v-else class="text-sm text-muted">Завершённых ранов за неделю нет.</p>
            </UCard>
          </section>
        </div>
      </template>
    </template>
  </UDashboardPanel>
</template>
