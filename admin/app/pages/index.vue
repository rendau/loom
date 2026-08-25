<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'
import { getDashboard } from '~/api/dashboard.api'
import { listRuns } from '~/api/run.api'
import type { Dashboard } from '~/types/dashboard'
import type { Run } from '~/types/run'

// Обзор — ответ на «что требует моего внимания» (design/05 §1):
// 1) провалы для разбора и зависшие раны; 2) что выполняется сейчас;
// 3) вторичное: активность, ближайшие запуски, пулы, длительности.
// Декоративных счётчиков нет — это операционный инструмент.

const data = ref<Dashboard | null>(null)
const activeRuns = ref<Run[]>([])
const loading = ref(false)
const loadError = ref('')

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    const [dashboard, running] = await Promise.all([
      getDashboard(),
      listRuns({ list_params: { page_size: 20, sort: ['created_at'] }, status: 'running' }),
    ])
    data.value = dashboard
    activeRuns.value = running.results
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

onMounted(load)
usePolling(() => load(true), 15_000)

const now = useTimeTick()

// «дольше обычного»: длительность running-рана превысила недельный
// максимум своего дага (dag_durations из dashboard RPC)
const maxByDag = computed(() => {
  const m = new Map<string, number>()
  for (const d of data.value?.dag_durations ?? []) {
    if (d.max_sec > 0)
      m.set(d.dag_name, d.max_sec)
  }
  return m
})

function runningSec(run: Run): number {
  return Math.max(0, (now.value - new Date(run.created_at).getTime()) / 1000)
}

function isSlow(run: Run): boolean {
  const max = maxByDag.value.get(run.dag_name)
  return max !== undefined && runningSec(run) > max
}

const slowRuns = computed(() => activeRuns.value.filter(isSlow))

const needsAttention = computed(() =>
  (data.value?.recent_failures?.length ?? 0) > 0 || slowRuns.value.length > 0)

// пулы: показываем только занятые и стоящие на паузе — свободные не
// требуют внимания и сворачиваются в одну строку
const busyPools = computed(() =>
  (data.value?.pools ?? []).filter(p => Number(p.busy) > 0 || Number(p.slots) === 0))
</script>

<template>
  <UDashboardPanel id="dashboard">
    <template #header>
      <UDashboardNavbar title="Обзор">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" aria-label="Обновить" @click="load()" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <template v-if="data">
        <!-- ── 1. Требует внимания ── -->
        <section>
          <SectionHeader title="Требует внимания" />
          <UCard :ui="{ body: 'p-3 sm:p-3' }">
            <div v-if="needsAttention" class="space-y-1.5">
              <NuxtLink
                v-for="run in data.recent_failures ?? []"
                :key="run.run_id"
                :to="`/runs/${encodeURIComponent(run.run_id)}`"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-circle-x" class="size-4 shrink-0 self-center text-error" />
                <span class="font-medium text-highlighted">{{ run.dag_name }}</span>
                <span class="text-muted">провал</span>
                <span class="truncate font-mono text-xs text-dimmed">{{ run.run_id }}</span>
                <span class="ms-auto shrink-0 text-xs text-muted"><RelativeTime :time="run.finished_at" /></span>
              </NuxtLink>
              <NuxtLink
                v-for="run in slowRuns"
                :key="run.id"
                :to="`/runs/${encodeURIComponent(run.id)}`"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-timer" class="size-4 shrink-0 self-center text-warning" />
                <span class="font-medium text-highlighted">{{ run.dag_name }}</span>
                <span class="text-muted">дольше обычного — идёт {{ formatDuration(run.created_at, undefined, now) }}</span>
                <span class="truncate font-mono text-xs text-dimmed">{{ run.id }}</span>
              </NuxtLink>
            </div>
            <p v-else class="flex items-center gap-2 px-2 py-1 text-sm text-muted">
              <UIcon name="i-lucide-circle-check" class="size-4 text-success" />
              Провалов нет, зависших ранов нет.
            </p>
          </UCard>
        </section>

        <!-- ── 2. Выполняется сейчас ── -->
        <section>
          <SectionHeader title="Выполняется сейчас" :count="activeRuns.length" />
          <UCard :ui="{ body: 'p-3 sm:p-3' }">
            <div v-if="activeRuns.length" class="space-y-1.5">
              <NuxtLink
                v-for="run in activeRuns"
                :key="run.id"
                :to="`/runs/${encodeURIComponent(run.id)}`"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-loader-circle" class="size-4 shrink-0 animate-spin self-center text-info" />
                <span class="font-medium text-highlighted">{{ run.dag_name }}</span>
                <span class="truncate font-mono text-xs text-dimmed">{{ run.id }}</span>
                <UBadge v-if="isSlow(run)" color="warning" variant="subtle" size="sm">дольше обычного</UBadge>
                <span class="ms-auto shrink-0 text-xs tabular-nums text-muted">
                  идёт {{ formatDuration(run.created_at, undefined, now) }}
                </span>
              </NuxtLink>
            </div>
            <p v-else class="px-2 py-1 text-sm text-muted">Активных ранов нет.</p>
          </UCard>
        </section>

        <!-- ── 3. Вторичное ── -->
        <section>
          <SectionHeader title="Активность за 2 недели" />
          <UCard :ui="{ body: 'p-4 sm:p-4' }">
            <DashboardActivityBars :days="data.activity ?? []" />
          </UCard>
        </section>

        <div class="grid gap-4 lg:grid-cols-2">
          <section>
            <SectionHeader title="Ближайшие запуски" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="data.upcoming?.length" class="space-y-2">
                <div v-for="item in data.upcoming" :key="item.dag_name" class="flex items-baseline justify-between gap-2 text-sm">
                  <NuxtLink :to="`/dags/${encodeURIComponent(item.dag_name)}`" class="truncate hover:text-primary hover:underline">
                    {{ item.dag_name }}
                  </NuxtLink>
                  <span class="shrink-0 text-xs text-muted">
                    <span class="font-mono">{{ item.schedule }}</span> · <RelativeTime :time="item.next_run_at" />
                  </span>
                </div>
              </div>
              <p v-else class="text-sm text-muted">Дагов с расписанием нет.</p>
            </UCard>
          </section>

          <section>
            <SectionHeader title="Пулы слотов" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="busyPools.length" class="space-y-2">
                <div v-for="pool in busyPools" :key="pool.name" class="space-y-1">
                  <div class="flex items-baseline justify-between gap-2 text-sm">
                    <NuxtLink to="/pools" class="truncate hover:text-primary hover:underline">{{ pool.name }}</NuxtLink>
                    <span class="shrink-0 text-xs text-muted">
                      <UBadge v-if="Number(pool.slots) === 0" color="warning" variant="subtle" size="sm">пауза</UBadge>
                      <template v-else>{{ pool.busy }} / {{ pool.slots }}</template>
                    </span>
                  </div>
                  <div v-if="Number(pool.slots) > 0" class="h-1.5 w-full overflow-hidden rounded-full bg-elevated">
                    <div
                      class="h-full rounded-full"
                      :class="Number(pool.busy) >= Number(pool.slots) ? 'bg-warning' : 'bg-primary'"
                      :style="{ width: `${Math.min(100, (Number(pool.busy) / Number(pool.slots)) * 100)}%` }"
                    />
                  </div>
                </div>
              </div>
              <p v-else class="text-sm text-muted">
                Все пулы свободны<template v-if="data.pools?.length"> ({{ data.pools.length }})</template>.
                <NuxtLink to="/pools" class="text-primary hover:underline">Пулы →</NuxtLink>
              </p>
            </UCard>
          </section>

          <section class="lg:col-span-2">
            <SectionHeader title="Длительность ранов (неделя)" />
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
