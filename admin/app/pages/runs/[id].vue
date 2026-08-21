<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { getRun } from '~/api/run.api'
import type { Attempt, Run, TaskInstance } from '~/types/run'
import type { RunLogSlideover } from '#components'

// Детали рана: статусы тасков и попыток; пока ран выполняется — авто-poll.

const route = useRoute()
const runId = String(route.params.id)

const run = ref<Run | null>(null)
const tasks = ref<TaskInstance[]>([])
const attempts = ref<Attempt[]>([])
const loading = ref(false)
const loadError = ref('')

async function load() {
  loading.value = true
  try {
    const rep = await getRun(runId)
    run.value = rep.run
    tasks.value = rep.tasks
    attempts.value = rep.attempts
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

// авто-обновление, пока ран живой
let pollTimer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  pollTimer = setInterval(() => {
    if (run.value?.status === 'running')
      load()
  }, 3000)
})
onBeforeUnmount(() => clearInterval(pollTimer))

// лог попытки
const logRef = ref<InstanceType<typeof RunLogSlideover> | null>(null)

function openTaskLog(ti: TaskInstance) {
  if (ti.attempt < 1)
    return
  logRef.value?.show(ti.task, ti.attempt, ti.status === 'running' || ti.status === 'starting')
}

function openAttemptLog(a: Attempt) {
  logRef.value?.show(a.task, a.attempt, a.status === 'running' || a.status === 'starting')
}

const taskColumns: TableColumn<TaskInstance>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'attempt', header: 'Попытка' },
  { accessorKey: 'started_at', header: 'Старт' },
  { accessorKey: 'finished_at', header: 'Завершён' },
  { id: 'duration', header: 'Длительность' },
  { id: 'actions', header: '' },
]

const attemptColumns: TableColumn<Attempt>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'attempt', header: '№' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'created_at', header: 'Создана' },
  { accessorKey: 'finished_at', header: 'Завершена' },
  { id: 'exit', header: 'Исход' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="run-details">
    <template #header>
      <UDashboardNavbar :title="runId">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/runs" />
        </template>
        <template #right>
          <UBadge v-if="run" :color="runStatusColor(run.status)" variant="subtle" size="lg">
            {{ runStatusLabel(run.status) }}
          </UBadge>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" class="mb-4" />

      <template v-if="run">
        <div class="mb-6 grid grid-cols-2 gap-x-8 gap-y-1 text-sm lg:grid-cols-4">
          <div>
            <div class="text-muted">Даг</div>
            <div class="font-medium">{{ run.dag_name }}</div>
          </div>
          <div>
            <div class="text-muted">Триггер</div>
            <div>{{ run.trigger === 'schedule' ? 'расписание' : 'вручную' }}</div>
          </div>
          <div>
            <div class="text-muted">Создан</div>
            <div>{{ formatDateTime(run.created_at) }}</div>
          </div>
          <div>
            <div class="text-muted">Длительность</div>
            <div>{{ formatDuration(run.created_at, run.finished_at) }}</div>
          </div>
          <div class="col-span-2 lg:col-span-4">
            <div class="text-muted">Образ</div>
            <div class="truncate font-mono text-xs">{{ run.image }}</div>
          </div>
        </div>

        <h3 class="mb-2 font-semibold text-highlighted">Таски</h3>
        <UTable :data="tasks" :columns="taskColumns" class="mb-6">
          <template #task-cell="{ row }">
            <span class="font-medium">{{ row.original.task }}</span>
          </template>
          <template #status-cell="{ row }">
            <div class="flex items-center gap-2">
              <UBadge :color="taskStatusColor(row.original.status)" variant="subtle">
                {{ taskStatusLabel(row.original.status) }}
              </UBadge>
              <span v-if="row.original.status === 'up_for_retry'" class="text-xs text-muted">
                ретрай в {{ formatTime(row.original.retry_at) }}
              </span>
            </div>
          </template>
          <template #attempt-cell="{ row }">
            {{ row.original.attempt || '—' }}
          </template>
          <template #started_at-cell="{ row }">
            {{ formatDateTime(row.original.started_at) }}
          </template>
          <template #finished_at-cell="{ row }">
            {{ formatDateTime(row.original.finished_at) }}
          </template>
          <template #duration-cell="{ row }">
            {{ formatDuration(row.original.started_at, row.original.finished_at) }}
          </template>
          <template #actions-cell="{ row }">
            <UButton
              v-if="row.original.attempt >= 1"
              icon="i-lucide-scroll-text"
              size="sm"
              color="neutral"
              variant="ghost"
              label="Лог"
              @click="openTaskLog(row.original)"
            />
          </template>
        </UTable>

        <h3 class="mb-2 font-semibold text-highlighted">Попытки</h3>
        <UTable :data="attempts" :columns="attemptColumns">
          <template #status-cell="{ row }">
            <UBadge :color="attemptStatusColor(row.original.status)" variant="subtle">
              {{ row.original.status }}
            </UBadge>
          </template>
          <template #created_at-cell="{ row }">
            {{ formatDateTime(row.original.created_at) }}
          </template>
          <template #finished_at-cell="{ row }">
            {{ formatDateTime(row.original.finished_at) }}
          </template>
          <template #exit-cell="{ row }">
            <span class="font-mono text-xs">
              <template v-if="row.original.exit_code !== undefined && row.original.exit_code !== null">
                code {{ row.original.exit_code }}
              </template>
              <template v-if="row.original.exit_reason">
                · {{ row.original.exit_reason }}
              </template>
            </span>
          </template>
          <template #actions-cell="{ row }">
            <UButton
              icon="i-lucide-scroll-text"
              size="sm"
              color="neutral"
              variant="ghost"
              label="Лог"
              @click="openAttemptLog(row.original)"
            />
          </template>
        </UTable>
      </template>

      <RunLogSlideover ref="logRef" :run-id="runId" />
    </template>
  </UDashboardPanel>
</template>
