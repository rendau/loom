<script setup lang="ts">
import { getStorageStats } from '~/api/artifact.api'
import { apiErrorMessage } from '~/api/client'
import { getDashboard } from '~/api/dashboard.api'
import { listProjects } from '~/api/project.api'
import { listRuns } from '~/api/run.api'
import type { StorageStats } from '~/types/artifact'
import type { Dashboard } from '~/types/dashboard'
import type { Project } from '~/types/project'
import type { Run } from '~/types/run'

// Обзор — ответ на «что требует моего внимания» (design/05 §1):
// 1) провалы для разбора и зависшие раны; 2) что выполняется сейчас;
// 3) вторичное: активность, ближайшие запуски, пулы, длительности.
// Декоративных счётчиков нет — это операционный инструмент.

const data = ref<Dashboard | null>(null)
const activeRuns = ref<Run[]>([])
// проекты (образы) — из чего собран парк дагов: сколько их, что
// обновляется само и сколько весит каждый образ
const projects = ref<Project[]>([])
const storage = ref<StorageStats | null>(null)
const loading = ref(false)
const loadError = ref('')

// даги с незаполненными переменными/секретами: состав задаёт код дага,
// значения — админка, и до первого падения launch_failed о разрыве никто
// не узнаёт. Меняется редко — обновляем не фоновым тиком, а вместе с
// явной загрузкой обзора.
const envGaps = useDagEnvGaps()

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    const [dashboard, running, projectList] = await Promise.all([
      getDashboard(),
      listRuns({ list_params: { page_size: 20, sort: ['created_at'] }, status: 'running' }),
      listProjects({ list_params: { page_size: 100, sort: ['name'] } }),
    ])
    data.value = dashboard
    activeRuns.value = running.results
    projects.value = projectList.results ?? []
    loadError.value = ''

    if (!background)
      await envGaps.load()

    // ёмкость хранилища — best effort (недоступный artifact-сервер не
    // валит обзор)
    try {
      storage.value = await getStorageStats()
    }
    catch {
      storage.value = null
    }
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
      m.set(dagRefLabel(runDagRef(d)), d.max_sec)
  }
  return m
})

function runningSec(run: Run): number {
  return Math.max(0, (now.value - new Date(run.created_at).getTime()) / 1000)
}

function isSlow(run: Run): boolean {
  const max = maxByDag.value.get(dagRefLabel(runDagRef(run)))
  return max !== undefined && runningSec(run) > max
}

const slowRuns = computed(() => activeRuns.value.filter(isSlow))

const envGapDags = computed(() => [...envGaps.gaps.value.entries()]
  .map(([label, missing]) => ({ dag: parseDagLabel(label), missing }))
  .sort((a, b) => b.missing - a.missing))

const needsAttention = computed(() =>
  (data.value?.recent_failures?.length ?? 0) > 0
  || slowRuns.value.length > 0
  || envGapDags.value.length > 0)

// доля занятого места на volume хранилища (для прогресса и подсветки)
const volumeUsedShare = computed(() => {
  const s = storage.value
  if (!s)
    return 0
  const total = Number(s.data.total_bytes)
  return total > 0 ? (total - Number(s.data.free_bytes)) / total : 0
})

// окно графика (дни или часы) выбирается по возрасту данных
const activity = computed(() =>
  activityWindow(data.value?.activity ?? [], data.value?.activity_hours ?? []))

// на обзоре — первые несколько проектов, остальное на своей странице
const PROJECTS_SHOWN = 6

const autoUpdateProjects = computed(() => projects.value.filter(p => p.auto_update).length)

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
        <!-- ── 1. Требует внимания: пусто — секции нет вовсе, чтобы обзор
                 начинался с того, что происходит сейчас ── -->
        <section v-if="needsAttention">
          <SectionHeader title="Требует внимания" />
          <UCard :ui="{ body: 'p-3 sm:p-3' }">
            <div class="space-y-1.5">
              <NuxtLink
                v-for="run in data.recent_failures ?? []"
                :key="run.run_id"
                :to="`/runs/${encodeURIComponent(run.run_id)}`"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-circle-x" class="size-4 shrink-0 self-center text-error" />
                <span class="font-medium text-highlighted">{{ run.dag_name }}</span>
                <span class="text-xs text-dimmed">{{ run.project }}</span>
                <template v-if="run.task">
                  <span class="text-muted">упал таск</span>
                  <span class="font-medium">{{ run.task }}</span>
                  <span v-if="run.exit_reason" class="truncate font-mono text-xs text-error">{{ run.exit_reason }}</span>
                </template>
                <span v-else class="text-muted">провал</span>
                <span class="truncate font-mono text-xs text-dimmed">{{ run.run_id }}</span>
                <span class="ms-auto shrink-0 text-xs text-muted"><RelativeTime :time="run.finished_at" /></span>
              </NuxtLink>
              <NuxtLink
                v-for="gap in envGapDags"
                :key="dagRefLabel(gap.dag)"
                :to="dagLink(gap.dag, 'tab=env')"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-key-round" class="size-4 shrink-0 self-center text-error" />
                <span class="font-medium text-highlighted">{{ gap.dag.name }}</span>
                <span class="text-xs text-dimmed">{{ gap.dag.project }}</span>
                <span class="text-muted">
                  не заполнено переменных и секретов: {{ gap.missing }} — таски упадут launch_failed
                </span>
              </NuxtLink>
              <NuxtLink
                v-for="run in slowRuns"
                :key="run.id"
                :to="`/runs/${encodeURIComponent(run.id)}`"
                class="flex items-baseline gap-2 rounded-md px-2 py-1 text-sm hover:bg-elevated"
              >
                <UIcon name="i-lucide-timer" class="size-4 shrink-0 self-center text-warning" />
                <span class="font-medium text-highlighted">{{ run.dag_name }}</span>
                <span class="text-xs text-dimmed">{{ run.project }}</span>
                <span class="text-muted">дольше обычного — идёт {{ formatDuration(run.created_at, undefined, now) }}</span>
                <span class="truncate font-mono text-xs text-dimmed">{{ run.id }}</span>
              </NuxtLink>
            </div>
          </UCard>
        </section>

        <!-- ── 2. Остальное — плиткой в две колонки: карточки разной
                 высоты укладываются без вертикальных дыр ── -->
        <div class="columns-1 gap-4 lg:columns-2 [&>section]:mb-4 [&>section]:break-inside-avoid">
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
                  <span class="text-xs text-dimmed">{{ run.project }}</span>
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

          <section>
            <SectionHeader :title="activity.title" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <DashboardActivityBars :points="activity.points" />
            </UCard>
          </section>

          <section>
            <SectionHeader title="Проекты" :count="projects.length" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="projects.length" class="space-y-2">
                <div
                  v-for="project in projects.slice(0, PROJECTS_SHOWN)"
                  :key="project.name"
                  class="flex items-baseline justify-between gap-2 text-sm"
                >
                  <NuxtLink
                    :to="`/projects/${encodeURIComponent(project.name)}`"
                    class="truncate hover:text-primary hover:underline"
                  >
                    {{ project.name }}
                  </NuxtLink>
                  <span class="flex shrink-0 items-baseline gap-2 text-xs text-muted">
                    <UTooltip v-if="project.auto_update" text="Авто-обновление: digest тега отслеживается в registry">
                      <UBadge color="info" variant="subtle" size="sm">auto</UBadge>
                    </UTooltip>
                    <span class="tabular-nums">дагов: {{ project.dag_count }}</span>
                    <span v-if="Number(project.image_size_bytes)" class="tabular-nums text-dimmed">
                      {{ formatBytes(project.image_size_bytes) }}
                    </span>
                  </span>
                </div>
                <div class="flex items-baseline justify-between gap-2 border-t border-default pt-2 text-xs text-muted">
                  <span>Авто-обновление у {{ autoUpdateProjects }} из {{ projects.length }}</span>
                  <NuxtLink to="/projects" class="text-primary hover:underline">
                    <template v-if="projects.length > PROJECTS_SHOWN">
                      ещё {{ projects.length - PROJECTS_SHOWN }} →
                    </template>
                    <template v-else>Все проекты →</template>
                  </NuxtLink>
                </div>
              </div>
              <p v-else class="text-sm text-muted">
                Проектов нет.
                <NuxtLink to="/projects" class="text-primary hover:underline">Зарегистрировать образ →</NuxtLink>
              </p>
            </UCard>
          </section>

          <section>
            <SectionHeader title="Ближайшие запуски" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div v-if="data.upcoming?.length" class="space-y-2">
                <div
                  v-for="item in data.upcoming"
                  :key="`${item.project}/${item.dag_name}`"
                  class="flex items-baseline justify-between gap-2 text-sm"
                >
                  <NuxtLink
                    :to="dagLink({ project: item.project, name: item.dag_name })"
                    class="truncate hover:text-primary hover:underline"
                  >
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

          <section v-if="storage">
            <SectionHeader title="Хранилище артефактов" />
            <UCard :ui="{ body: 'p-4 sm:p-4' }">
              <div class="space-y-2 text-sm">
                <div class="flex items-baseline justify-between gap-2">
                  <span>Артефакты</span>
                  <span class="text-xs tabular-nums text-muted">{{ formatBytes(storage.data.used_bytes) }}</span>
                </div>
                <div class="flex items-baseline justify-between gap-2">
                  <span>Логи тасков</span>
                  <span class="text-xs tabular-nums text-muted">{{ formatBytes(storage.logs.used_bytes) }}</span>
                </div>
                <div class="flex items-baseline justify-between gap-2 border-t border-default pt-2">
                  <span class="font-medium">Всего</span>
                  <span class="text-xs font-medium tabular-nums">
                    {{ formatBytes(Number(storage.data.used_bytes) + Number(storage.logs.used_bytes)) }}
                  </span>
                </div>
                <div class="flex items-baseline justify-between gap-2">
                  <span class="text-muted">Свободно на volume</span>
                  <span class="text-xs tabular-nums text-muted">
                    {{ formatBytes(storage.data.free_bytes) }} из {{ formatBytes(storage.data.total_bytes) }}
                  </span>
                </div>
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-elevated">
                  <div
                    class="h-full rounded-full"
                    :class="volumeUsedShare > 0.9 ? 'bg-error' : volumeUsedShare > 0.75 ? 'bg-warning' : 'bg-primary'"
                    :style="{ width: `${Math.min(100, volumeUsedShare * 100)}%` }"
                  />
                </div>
              </div>
            </UCard>
          </section>

          <section>
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
