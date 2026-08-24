<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { cancelRun, getRun, listRunValues, retryTask } from '~/api/run.api'
import type { DagTask } from '~/types/dag'
import type { Attempt, Run, TaskInstance, TaskValue } from '~/types/run'
import type { RunLogSlideover } from '#components'

// Детали рана: граф дага, статусы тасков и попыток; пока ран выполняется —
// авто-poll.

const route = useRoute()
const runId = String(route.params.id)

const run = ref<Run | null>(null)
const tasks = ref<TaskInstance[]>([])
const attempts = ref<Attempt[]>([])
const manifestTasks = ref<DagTask[]>([])
const loading = ref(false)
const loadError = ref('')

const values = ref<TaskValue[]>([])

// background — фоновый рефреш поллинга: без спиннера, чтобы кнопка
// обновления не мигала каждые 3 секунды
async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    const rep = await getRun(runId)
    run.value = rep.run
    tasks.value = rep.tasks
    attempts.value = rep.attempts
    manifestTasks.value = rep.manifest_tasks ?? []
    loadError.value = ''

    const valuesRep = await listRunValues(runId)
    values.value = valuesRep.values ?? []
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    if (!background)
      loading.value = false
  }
}

function formatValue(v: unknown): string {
  const s = JSON.stringify(v)
  return s.length > 200 ? `${s.slice(0, 199)}…` : s
}

// авто-обновление, пока ран живой
let pollTimer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  pollTimer = setInterval(() => {
    if (run.value?.status === 'running')
      load(true)
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

// ретрай таска: доступен на завершённом ране для исполнявшихся тасков
// (failed | success | canceled); upstream_failed не исполнялся — ретраить нечего
const action = useApiAction()
const retryTarget = ref<TaskInstance | null>(null)
const { canManageDag } = useAuth()

function canRetry(ti: TaskInstance): boolean {
  return run.value?.status !== 'running'
    && (ti.status === 'failed' || ti.status === 'success' || ti.status === 'canceled')
    && canManageDag(run.value?.dag_name)
}

// принудительная остановка рана: живые таски убиваются, незавершённые
// получают canceled; успешные остаются — ран можно доиграть ретраем
const cancelOpen = ref(false)

const canCancel = computed(() =>
  run.value?.status === 'running' && canManageDag(run.value?.dag_name))

async function confirmCancel() {
  const ok = await action.run(
    () => cancelRun(runId),
    { success: 'Ран остановлен' },
  )
  if (ok !== undefined) {
    cancelOpen.value = false
    await load()
  }
}

async function confirmRetry() {
  const target = retryTarget.value
  if (!target)
    return
  const ok = await action.run(
    () => retryTask(runId, target.task),
    { success: `Таск ${target.task} отправлен на ретрай` },
  )
  if (ok !== undefined) {
    retryTarget.value = null
    await load()
  }
}

const taskColumns: TableColumn<TaskInstance>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'attempt', header: 'Попытка' },
  { accessorKey: 'started_at', header: 'Старт' },
  { accessorKey: 'finished_at', header: 'Завершён' },
  { id: 'duration', header: 'Длительность' },
  { id: 'memory', header: 'Пик памяти' },
  { id: 'actions', header: '' },
]

// пик памяти текущей попытки таска (attempts содержит все попытки)
const peakByTask = computed(() => {
  const result: Record<string, number> = {}
  for (const a of attempts.value) {
    const ti = tasks.value.find(t => t.task === a.task)
    if (!ti || a.attempt !== ti.attempt || a.peak_memory_bytes === undefined)
      continue
    result[a.task] = Number(a.peak_memory_bytes)
  }
  return result
})

// агрегаты рана: max и сумма пиков по текущим попыткам тасков
const memorySummary = computed(() => {
  const peaks = Object.values(peakByTask.value)
  if (peaks.length === 0)
    return null
  return {
    max: Math.max(...peaks),
    sum: peaks.reduce((s, v) => s + v, 0),
  }
})

const attemptColumns: TableColumn<Attempt>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'attempt', header: '№' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'created_at', header: 'Создана' },
  { accessorKey: 'finished_at', header: 'Завершена' },
  { id: 'memory', header: 'Пик памяти' },
  { id: 'exit', header: 'Исход' },
  { id: 'actions', header: '' },
]

const valueColumns: TableColumn<TaskValue>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'key', header: 'Ключ' },
  { accessorKey: 'value', header: 'Значение' },
  { accessorKey: 'modified_at', header: 'Обновлено' },
]

// снятие whitespace-nowrap темы UTable: длинные русские статусы и значения
// должны переноситься, а не растягивать таблицу в горизонтальный скролл
const tableUi = { td: 'whitespace-normal' }
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
          <UTooltip v-if="canCancel" text="Остановить ран">
            <UButton icon="i-lucide-circle-stop" color="error" variant="ghost" @click="cancelOpen = true" />
          </UTooltip>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load()" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" />

      <template v-if="run">
        <!-- вертикальный ритм секций — только gap слота #body, без mb-* -->
        <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm lg:grid-cols-4">
          <div>
            <div class="text-muted">Даг</div>
            <div class="font-medium">{{ run.dag_name }}</div>
          </div>
          <div>
            <div class="text-muted">Триггер</div>
            <div>{{ runTriggerLabel(run.trigger) }}</div>
          </div>
          <div>
            <div class="text-muted">Логическая дата</div>
            <div>{{ formatDateTime(run.logical_date) }}</div>
          </div>
          <div>
            <div class="text-muted">Создан</div>
            <div>{{ formatDateTime(run.created_at) }}</div>
          </div>
          <div>
            <div class="text-muted">Длительность</div>
            <div>{{ formatDuration(run.created_at, run.finished_at) }}</div>
          </div>
          <div>
            <div class="text-muted">Ран</div>
            <CopyText :text="runId" mono />
          </div>
          <div v-if="memorySummary">
            <div class="text-muted">Память (max / Σ пиков)</div>
            <div>{{ formatBytes(memorySummary.max) }} / {{ formatBytes(memorySummary.sum) }}</div>
          </div>
          <div class="col-span-2">
            <div class="text-muted">Образ</div>
            <CopyText :text="run.image" mono />
          </div>
          <div v-if="run.params" class="col-span-2 lg:col-span-4">
            <UCollapsible>
              <UButton
                label="Параметры рана"
                color="neutral"
                variant="link"
                trailing-icon="i-lucide-chevron-down"
                class="group p-0"
                :ui="{ trailingIcon: 'transition-transform duration-200 group-data-[state=open]:rotate-180' }"
              />
              <template #content>
                <pre class="mt-1 max-h-64 overflow-auto rounded-md border border-default bg-muted/30 p-2 font-mono text-xs">{{ JSON.stringify(run.params, null, 2) }}</pre>
              </template>
            </UCollapsible>
          </div>
        </div>

        <section v-if="manifestTasks.length">
          <h3 class="mb-2 font-semibold text-highlighted">Граф</h3>
          <RunDagGraph
            :manifest-tasks="manifestTasks"
            :tasks="tasks"
            @open-log="openTaskLog"
          />
        </section>

        <section>
          <h3 class="mb-2 font-semibold text-highlighted">Таски</h3>
          <UTable :data="tasks" :columns="taskColumns" :ui="tableUi">
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
          <template #memory-cell="{ row }">
            {{ formatBytes(peakByTask[row.original.task]) }}
          </template>
          <template #actions-cell="{ row }">
            <div class="flex justify-end gap-1">
              <UTooltip v-if="canRetry(row.original)" text="Ретрай таска">
                <UButton
                  icon="i-lucide-rotate-ccw"
                  size="sm"
                  color="neutral"
                  variant="ghost"
                  @click="retryTarget = row.original"
                />
              </UTooltip>
              <UTooltip v-if="row.original.attempt >= 1" text="Лог таска">
                <UButton
                  icon="i-lucide-scroll-text"
                  size="sm"
                  color="neutral"
                  variant="ghost"
                  @click="openTaskLog(row.original)"
                />
              </UTooltip>
            </div>
          </template>
          </UTable>
        </section>

        <section v-if="values.length">
          <h3 class="mb-2 font-semibold text-highlighted">Значения</h3>
          <UTable :data="values" :columns="valueColumns" :ui="tableUi">
            <template #task-cell="{ row }">
              <span class="font-medium">{{ row.original.task }}</span>
            </template>
            <template #key-cell="{ row }">
              <span class="font-mono text-xs">{{ row.original.key }}</span>
            </template>
            <template #value-cell="{ row }">
              <div class="max-w-lg break-all font-mono text-xs" :title="formatValue(row.original.value)">
                {{ formatValue(row.original.value) }}
              </div>
            </template>
            <template #modified_at-cell="{ row }">
              {{ formatDateTime(row.original.modified_at) }}
            </template>
          </UTable>
        </section>

        <section>
          <h3 class="mb-2 font-semibold text-highlighted">Попытки</h3>
          <UTable :data="attempts" :columns="attemptColumns" :ui="tableUi">
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
          <template #memory-cell="{ row }">
            {{ formatBytes(row.original.peak_memory_bytes) }}
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
            <UTooltip text="Лог попытки">
              <UButton
                icon="i-lucide-scroll-text"
                size="sm"
                color="neutral"
                variant="ghost"
                @click="openAttemptLog(row.original)"
              />
            </UTooltip>
          </template>
          </UTable>
        </section>
      </template>

      <RunLogSlideover ref="logRef" :run-id="runId" />

      <!-- подтверждение остановки рана -->
      <UModal :open="cancelOpen" title="Остановить ран?" @update:open="cancelOpen = false">
        <template #body>
          <p>
            Выполняющиеся таски будут убиты, а незавершённые — получат статус
            «остановлен». Успешные таски останутся успешными: ран можно будет
            доиграть ретраем таска.
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="cancelOpen = false" />
            <UButton color="error" label="Остановить" :loading="action.loading.value" @click="confirmCancel" />
          </div>
        </template>
      </UModal>

      <!-- подтверждение ретрая -->
      <UModal :open="retryTarget !== null" title="Ретрай таска?" @update:open="retryTarget = null">
        <template #body>
          <p>
            Таск <span class="font-mono font-medium">{{ retryTarget?.task }}</span> уйдёт в очередь
            новой попыткой, его downstream-подграф будет сброшен и выполнится заново.
            Ран снова станет выполняющимся.
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="retryTarget = null" />
            <UButton color="primary" label="Ретрай" :loading="action.loading.value" @click="confirmRetry" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
