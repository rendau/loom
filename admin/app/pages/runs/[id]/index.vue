<script setup lang="ts">
import type { TableColumn, TableRow } from '@nuxt/ui'
import { listRunArtifacts } from '~/api/artifact.api'
import { apiErrorMessage } from '~/api/client'
import { cancelRun, getRun, listRunValues, retryTask } from '~/api/run.api'
import { listSecrets } from '~/api/secret.api'
import { listVariables } from '~/api/variable.api'
import type { ArtifactMain } from '~/types/artifact'
import type { DagTask } from '~/types/dag'
import type { Attempt, Run, RunEnv, TaskInstance, TaskValue } from '~/types/run'
import type { RunEnvBinding } from '~/utils/runenv'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Страница рана — master-detail (design/05 §5): компактный список тасков +
// инспектор выбранного таска (лог/попытки/значения/env). У failed-рана
// первый упавший таск выбирается автоматически — лог виден без кликов.
// Выбор и таба — в URL (?task=&tab=), ссылку можно переслать.

const route = useRoute()
const router = useRouter()
const runId = String(route.params.id)

const run = ref<Run | null>(null)
const tasks = ref<TaskInstance[]>([])
const attempts = ref<Attempt[]>([])
const manifestTasks = ref<DagTask[]>([])
const values = ref<TaskValue[]>([])
const runEnv = ref<RunEnv[]>([])
const artifacts = ref<ArtifactMain[]>([])
const loading = ref(false)
const loadError = ref('')

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
    runEnv.value = rep.env ?? []
    loadError.value = ''

    const valuesRep = await listRunValues(runId)
    values.value = valuesRep.values ?? []

    // артефакты — best effort: недоступный artifact-сервер не валит страницу
    try {
      artifacts.value = (await listRunArtifacts(runId)).results ?? []
    }
    catch {
      artifacts.value = []
    }

    autoSelectFailed()
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    if (!background)
      loading.value = false
  }
}

// env-контекст: переменные и секреты всех скоупов — для резолва привязок
// тасков (локальный перекрывает глобальный). Значения — текущие, снапшота
// на момент launch в API нет (design/07 №3) — EnvTable показывает пометку.
const variables = ref<Variable[]>([])
const secrets = ref<SecretMeta[]>([])

async function loadEnv() {
  try {
    const [v, s] = await Promise.all([listVariables(), listSecrets()])
    variables.value = v.results ?? []
    secrets.value = s.results ?? []
  }
  catch {
    // env-таба покажет привязки без значений — не критично для остального
  }
}

// авто-обновление, пока ран живой
onMounted(async () => {
  await Promise.all([load(), loadEnv()])
})
usePolling(() => load(true), 3000, () => run.value?.status === 'running')

// ── выбор таска и таба инспектора (в URL) ───────────────

const selectedTask = ref(typeof route.query.task === 'string' ? route.query.task : '')
const inspectorTab = ref(typeof route.query.tab === 'string' ? route.query.tab : 'log')

// автовыбор первого упавшего — только если пользователь ничего не выбрал
// сам (и не пришёл по ссылке с ?task=); фиксируется после первого раза,
// чтобы поллинг не перепрыгивал выбор
let autoSelected = selectedTask.value !== ''

function autoSelectFailed() {
  if (autoSelected || run.value?.status === 'running')
    return
  const failed = tasks.value.find(t => t.status === 'failed')
  if (failed)
    selectedTask.value = failed.task
  autoSelected = true
}

watch([selectedTask, inspectorTab], () => {
  const query: Record<string, string> = { ...route.query } as Record<string, string>
  delete query.task
  delete query.tab
  if (selectedTask.value) {
    query.task = selectedTask.value
    if (inspectorTab.value !== 'log')
      query.tab = inspectorTab.value
  }
  router.replace({ query })
})

const selectedTi = computed(() => tasks.value.find(t => t.task === selectedTask.value) ?? null)
const selectedManifest = computed(() => manifestTasks.value.find(t => t.name === selectedTask.value))

const tiByName = computed(() => new Map(tasks.value.map(t => [t.task, t])))

const selectedAttempts = computed(() =>
  attempts.value.filter(a => a.task === selectedTask.value).sort((a, b) => a.attempt - b.attempt))

const selectedValues = computed(() => values.value.filter(v => v.task === selectedTask.value))

const selectedArtifacts = computed(() => artifacts.value.filter(a => a.task === selectedTask.value))

// снапшот run_env есть (ран запускался после его введения) — env-табы
// показывают фактическую инъекцию; иначе fallback: текущие значения с
// пометкой
const hasEnvSnapshot = computed(() => runEnv.value.length > 0)

function snapshotBindings(keysFilter?: Set<string>): RunEnvBinding[] {
  return runEnv.value
    .filter(e => !keysFilter || keysFilter.has(`${e.kind}:${e.env}`))
    .map(e => ({
      env: e.env,
      kind: e.kind,
      name: e.name,
      scope: e.scope,
      value: e.kind === 'variable' ? e.value : undefined,
    }))
}

// ключи привязок манифеста таска — для среза снапшота по таску
function taskBindingKeys(mt: DagTask): Set<string> {
  const keys = new Set<string>()
  for (const v of mt.variables ?? [])
    keys.add(`variable:${v.env}`)
  for (const sec of mt.secrets ?? [])
    keys.add(`secret:${sec.env}`)
  return keys
}

const selectedBindings = computed(() => {
  const mt = selectedManifest.value
  if (!mt || !run.value)
    return []
  if (hasEnvSnapshot.value)
    return snapshotBindings(taskBindingKeys(mt))
  return resolveEnvBindings([mt], run.value.dag_name, variables.value, secrets.value)
})

const selectedDeps = computed(() =>
  (selectedManifest.value?.depends_on ?? []).map(d => ({
    task: d.task,
    streamed: d.streamed,
    status: tiByName.value.get(d.task)?.status,
  })))

// окружение всего рана — снапшот целиком, либо объединение привязок
const runBindings = computed(() => {
  if (hasEnvSnapshot.value)
    return snapshotBindings()
  if (!run.value || manifestTasks.value.length === 0)
    return []
  return resolveEnvBindings(manifestTasks.value, run.value.dag_name, variables.value, secrets.value)
})

function selectTask(name: string) {
  selectedTask.value = name
}

// клавиатура (design/05): ↑/↓ — по таскам, Esc — закрыть инспектор.
// Не срабатывает в полях ввода, модалках и на ручке ресайза.
function onKeydown(e: KeyboardEvent) {
  const target = e.target
  if (target instanceof Element
    && target.closest('input, textarea, select, [contenteditable], [role="dialog"], [role="separator"]'))
    return

  if (e.key === 'Escape') {
    if (selectedTask.value) {
      selectedTask.value = ''
      e.preventDefault()
    }
    return
  }

  if ((e.key !== 'ArrowUp' && e.key !== 'ArrowDown') || tasks.value.length === 0)
    return
  const idx = tasks.value.findIndex(t => t.task === selectedTask.value)
  const next = e.key === 'ArrowDown'
    ? Math.min(tasks.value.length - 1, idx + 1)
    : Math.max(0, idx <= 0 ? 0 : idx - 1)
  selectedTask.value = tasks.value[next]!.task
  e.preventDefault()
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

// ── список тасков ───────────────────────────────────────

const now = useTimeTick()

function taskDuration(ti: TaskInstance): string {
  const live = ti.status === 'running' || ti.status === 'starting'
  return formatDuration(ti.started_at, ti.finished_at, live ? now.value : undefined)
}

const taskColumns: TableColumn<TaskInstance>[] = [
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'task', header: 'Таск' },
  { accessorKey: 'attempt', header: 'Попытка' },
  { accessorKey: 'started_at', header: 'Старт' },
  { id: 'duration', header: 'Длительность' },
  { id: 'memory', header: 'Пик памяти' },
  { id: 'actions', header: '' },
]

function onTaskRowSelect(_e: Event, row: TableRow<TaskInstance>) {
  selectTask(row.original.task)
}

// пик памяти текущей попытки таска (attempts содержит все попытки)
const peakByTask = computed(() => {
  const result: Record<string, number> = {}
  for (const a of attempts.value) {
    const ti = tiByName.value.get(a.task)
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

// ── граф (сворачиваемый, состояние запоминается) ────────

const GRAPH_KEY = 'loom-run-graph-open'
const graphOpen = ref(localStorage.getItem(GRAPH_KEY) !== '0')
watch(graphOpen, v => localStorage.setItem(GRAPH_KEY, v ? '1' : '0'))

// ── действия ────────────────────────────────────────────

const action = useApiAction()
const { canManageDag } = useAuth()

// ретрай таска: доступен на завершённом ране для исполнявшихся тасков
// (failed | success | canceled); upstream_failed не исполнялся — ретраить нечего
const retryTarget = ref<TaskInstance | null>(null)

function canRetry(ti: TaskInstance | null): boolean {
  return !!ti && run.value?.status !== 'running'
    && (ti.status === 'failed' || ti.status === 'success' || ti.status === 'canceled')
    && canManageDag(run.value?.dag_name)
}

// downstream-подграф таска (сбрасывается ретраем) — по снапшоту манифеста
function downstreamOf(task: string): string[] {
  const dependents = new Map<string, string[]>()
  for (const t of manifestTasks.value) {
    for (const d of t.depends_on ?? []) {
      const list = dependents.get(d.task) ?? []
      list.push(t.name)
      dependents.set(d.task, list)
    }
  }
  const out: string[] = []
  const queue = [task]
  const seen = new Set<string>([task])
  while (queue.length) {
    for (const dep of dependents.get(queue.shift()!) ?? []) {
      if (seen.has(dep))
        continue
      seen.add(dep)
      out.push(dep)
      queue.push(dep)
    }
  }
  return out
}

const retryDownstream = computed(() =>
  retryTarget.value ? downstreamOf(retryTarget.value.task) : [])

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
</script>

<template>
  <UDashboardPanel id="run-details">
    <template #header>
      <UDashboardNavbar :title="run ? `${run.dag_name} / ${runId}` : runId">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/runs" aria-label="К списку ранов" />
        </template>
        <template #right>
          <StatusBadge v-if="run" kind="run" :status="run.status" size="lg" />
          <UTooltip v-if="canCancel" text="Остановить ран">
            <UButton icon="i-lucide-circle-stop" color="error" variant="ghost" aria-label="Остановить ран" @click="cancelOpen = true" />
          </UTooltip>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" aria-label="Обновить" @click="load()" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки рана"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <template v-if="run">
        <UAlert
          v-if="run.status === 'canceled'"
          color="neutral"
          variant="subtle"
          icon="i-lucide-circle-slash"
          title="Ран остановлен"
          description="Успешные таски сохранены — ран можно доиграть ретраем нужного таска."
        />

        <!-- вертикальный ритм секций — только gap слота #body, без mb-* -->
        <MetaGrid>
          <MetaItem label="Даг">
            <NuxtLink :to="`/dags/${encodeURIComponent(run.dag_name)}`" class="font-medium text-highlighted hover:text-primary hover:underline">
              {{ run.dag_name }}
            </NuxtLink>
          </MetaItem>
          <MetaItem label="Триггер">{{ runTriggerLabel(run.trigger) }}</MetaItem>
          <MetaItem label="Дата данных">{{ formatDateTime(run.logical_date) }}</MetaItem>
          <MetaItem label="Запущен">{{ formatDateTime(run.created_at) }}</MetaItem>
          <MetaItem label="Длительность">
            {{ formatDuration(run.created_at, run.finished_at, run.status === 'running' ? now : undefined) }}
          </MetaItem>
          <MetaItem label="Ран">
            <CopyText :text="runId" mono />
          </MetaItem>
          <MetaItem v-if="memorySummary" label="Память (max / Σ пиков)">
            {{ formatBytes(memorySummary.max) }} / {{ formatBytes(memorySummary.sum) }}
          </MetaItem>
          <MetaItem label="Образ" span>
            <CopyText :text="run.image" mono />
          </MetaItem>
          <div class="col-span-2 flex items-baseline gap-6 lg:col-span-4">
            <UCollapsible v-if="run.params">
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
            <UCollapsible v-if="runBindings.length" class="min-w-0 flex-1">
              <UButton
                :label="`Окружение рана (${runBindings.length})`"
                color="neutral"
                variant="link"
                trailing-icon="i-lucide-chevron-down"
                class="group p-0"
                :ui="{ trailingIcon: 'transition-transform duration-200 group-data-[state=open]:rotate-180' }"
              />
              <template #content>
                <RunEnvTable :bindings="runBindings" :snapshot="hasEnvSnapshot" class="mt-1" />
              </template>
            </UCollapsible>
          </div>
        </MetaGrid>

        <section v-if="manifestTasks.length">
          <SectionHeader title="Граф">
            <UButton
              :icon="graphOpen ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
              size="xs"
              color="neutral"
              variant="ghost"
              :aria-label="graphOpen ? 'Свернуть граф' : 'Развернуть граф'"
              @click="graphOpen = !graphOpen"
            />
          </SectionHeader>
          <RunDagGraph
            v-if="graphOpen"
            :manifest-tasks="manifestTasks"
            :tasks="tasks"
            :selected="selectedTask"
            compact
            @select="ti => selectTask(ti.task)"
          />
        </section>

        <section>
          <SectionHeader title="Таски" :count="tasks.length" />
          <UTable
            :data="tasks"
            :columns="taskColumns"
            :ui="{ ...denseTableUi, tr: 'cursor-pointer' }"
            @select="onTaskRowSelect"
          >
            <template #status-cell="{ row }">
              <div class="flex items-center gap-2">
                <StatusBadge kind="task" :status="row.original.status" size="sm" />
                <span v-if="row.original.status === 'up_for_retry'" class="text-xs text-muted">
                  ретрай в {{ formatTime(row.original.retry_at) }}
                </span>
              </div>
            </template>
            <template #task-cell="{ row }">
              <!-- маркер выбора — нейтральный (primary совпадает с
                   success-зелёным и на failed-таске врал бы про статус) -->
              <span class="flex items-center gap-1 font-medium text-highlighted">
                <UIcon
                  v-if="row.original.task === selectedTask"
                  name="i-lucide-chevron-right"
                  class="size-3.5 shrink-0"
                />
                {{ row.original.task }}
              </span>
            </template>
            <template #attempt-cell="{ row }">
              {{ row.original.attempt || '—' }}
            </template>
            <template #started_at-cell="{ row }">
              <RelativeTime :time="row.original.started_at" />
            </template>
            <template #duration-cell="{ row }">
              <span class="whitespace-nowrap tabular-nums">{{ taskDuration(row.original) }}</span>
            </template>
            <template #memory-cell="{ row }">
              {{ formatBytes(peakByTask[row.original.task]) }}
            </template>
            <template #actions-cell="{ row }">
              <div class="flex justify-end">
                <UTooltip v-if="canRetry(row.original)" text="Ретрай таска">
                  <UButton
                    icon="i-lucide-rotate-ccw"
                    size="sm"
                    color="neutral"
                    variant="ghost"
                    aria-label="Ретрай таска"
                    @click="retryTarget = row.original"
                  />
                </UTooltip>
              </div>
            </template>
          </UTable>
        </section>

        <RunTaskInspector
          v-if="selectedTi"
          v-model:tab="inspectorTab"
          :run-id="runId"
          :ti="selectedTi"
          :manifest-task="selectedManifest"
          :attempts="selectedAttempts"
          :values="selectedValues"
          :bindings="selectedBindings"
          :env-snapshot="hasEnvSnapshot"
          :artifacts="selectedArtifacts"
          :deps="selectedDeps"
          :can-retry="canRetry(selectedTi)"
          @close="selectedTask = ''"
          @retry="retryTarget = selectedTi"
        />
        <p v-else class="text-sm text-muted">
          Выберите таск в списке или на графе — здесь откроются его лог, попытки, значения и окружение.
        </p>
      </template>

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
            новой попыткой. Ран снова станет выполняющимся.
          </p>
          <p v-if="retryDownstream.length" class="mt-2">
            Downstream-подграф будет сброшен и выполнится заново:
            <span class="font-mono">{{ retryDownstream.join(', ') }}</span>.
          </p>
          <p v-else class="mt-2 text-muted">Downstream-тасков нет — перезапустится только сам таск.</p>
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
