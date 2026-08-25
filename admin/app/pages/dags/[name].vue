<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow, TabsItem } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { getDag, getDagStats, listDagRegistrations, listTaskResources, setDagAutoUpdate, setDagPaused, syncDag } from '~/api/dag.api'
import { listRuns } from '~/api/run.api'
import type { Dag, DagRegistration, DagTask, DagTaskStat, TaskResourcesOverride } from '~/types/dag'
import type { Run } from '~/types/run'

// Карточка дага — два лица (design/02): таба «Обзор» — как даг себя ведёт
// (последние раны, таски по ресурсам), таба «Схема» — как устроен (граф,
// манифест, история регистраций, служебное). Обзор — дефолт: поведение
// смотрят чаще устройства.

const route = useRoute()
const router = useRouter()
const dagName = String(route.params.name)

const { isAdmin, canManageDag } = useAuth()
const canManage = computed(() => canManageDag(dagName))

const dag = ref<Dag | null>(null)
const registrations = ref<DagRegistration[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

const isUpdating = computed(() =>
  registrations.value.some(r => r.status === 'pending' || r.status === 'running'))

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    dag.value = await getDag(dagName)
    registrations.value = (await listDagRegistrations({ dag_name: dagName, limit: 20 })).results
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

// ── табы (?tab=): overview — дефолт ─────────────────────

const knownTabs = ['overview', 'schema', 'settings']
const tab = ref(knownTabs.includes(String(route.query.tab)) ? String(route.query.tab) : 'overview')

const tabItems: TabsItem[] = [
  { label: 'Обзор', value: 'overview', icon: 'i-lucide-activity' },
  { label: 'Схема', value: 'schema', icon: 'i-lucide-workflow' },
  { label: 'Настройки', value: 'settings', icon: 'i-lucide-settings-2' },
]

watch(tab, () => {
  const query = { ...route.query } as Record<string, string>
  delete query.tab
  if (tab.value !== 'overview')
    query.tab = tab.value
  router.replace({ query })
})

// ── последние раны и таски по ресурсам (таба «Обзор») ───

const runs = ref<Run[]>([])
const runsError = ref('')

async function loadRuns() {
  try {
    const rep = await listRuns({
      list_params: { page_size: 20, sort: ['-created_at'] },
      dag_name: dagName,
    })
    runs.value = rep.results
    runsError.value = ''
  }
  catch (error) {
    runsError.value = apiErrorMessage(error)
  }
}

// «жирные таски»: агрегат по последним завершённым ранам (GetDagStats)
const taskStats = ref<DagTaskStat[]>([])
const statsRuns = ref(0)

async function loadStats() {
  try {
    const rep = await getDagStats(dagName)
    statsRuns.value = Number(rep.runs)
    taskStats.value = [...(rep.tasks ?? [])].sort((a, b) =>
      Number(b.max_peak_memory_bytes ?? -1) - Number(a.max_peak_memory_bytes ?? -1)
      || b.avg_duration_sec - a.avg_duration_sec)
  }
  catch {
    // секция просто останется пустой — не критично
  }
}

// ── оверрайды ресурсов тасков (админка приоритетнее кода) ──

const resourceOverrides = ref<TaskResourcesOverride[]>([])

async function loadOverrides() {
  try {
    resourceOverrides.value = (await listTaskResources(dagName)).results ?? []
  }
  catch {
    // секция останется на значениях манифеста — не критично
  }
}

const overrideByTask = computed(() =>
  new Map(resourceOverrides.value.map(o => [o.task, o])))

// эффективный лимит памяти: оверрайд из админки перекрывает манифест
const memoryLimitByTask = computed(() =>
  new Map((dag.value?.tasks ?? []).map((t) => {
    const override = overrideByTask.value.get(t.name)?.memory_limit
    return [t.name, {
      value: override || t.resources?.memory_limit || undefined,
      overridden: Boolean(override),
    }]
  })))

// редактор оверрайда: открывается по имени таска из обеих таб
const resourcesTarget = ref<DagTask | null>(null)

function openResources(taskName: string) {
  resourcesTarget.value = dag.value?.tasks.find(t => t.name === taskName) ?? null
}

// пока идёт регистрация/обновление — частый поллинг карточки; раны
// обновляются фоновым тиком на табе «Обзор»
onMounted(async () => {
  await Promise.all([load(), loadRuns(), loadStats(), loadOverrides()])
})
usePolling(() => load(true), 3000, () => isUpdating.value)
usePolling(() => {
  loadRuns()
  loadStats()
}, 10_000, () => tab.value === 'overview')

const runColumns: TableColumn<Run>[] = [
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'logical_date', header: 'Дата данных' },
  { id: 'started', header: 'Запущен' },
  { id: 'duration', header: 'Длительность' },
]

function openRun(_e: Event, row: TableRow<Run>) {
  navigateTo(`/runs/${row.original.id}`)
}

const nowTick = useTimeTick()

const statColumns: TableColumn<DagTaskStat>[] = [
  { accessorKey: 'task', header: 'Таск' },
  { id: 'duration', header: 'Длительность (ср/макс)' },
  { id: 'memory', header: 'Пик памяти (ср/макс)' },
  { id: 'limit', header: 'Лимит памяти' },
]

// ── действия ────────────────────────────────────────────

const triggerTarget = ref<Dag | null>(null)
const backfillTarget = ref<Dag | null>(null)
const scheduleTarget = ref<Dag | null>(null)
const deleteTarget = ref<Dag | null>(null)

async function onScheduleSaved() {
  scheduleTarget.value = null
  await load()
}

async function onDeleted() {
  deleteTarget.value = null
  await navigateTo('/dags')
}

async function togglePaused() {
  const d = dag.value
  if (!d)
    return
  const ok = await action.run(
    () => setDagPaused(d.name, !d.paused),
    { success: d.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
  )
  if (ok !== undefined)
    await load()
}

// принудительное обновление дага из registry: перерегистрация его текущего
// образа сейчас, не дожидаясь тика авто-обновления; статус видно в истории
// регистраций (и бейджем «обновляется»)
async function sync() {
  const d = dag.value
  if (!d)
    return
  const ok = await action.run(
    () => syncDag(d.name),
    { success: 'Обновление дага поставлено в очередь' },
  )
  if (ok !== undefined)
    await load(true)
}

async function toggleAutoUpdate() {
  const d = dag.value
  if (!d)
    return
  const ok = await action.run(
    () => setDagAutoUpdate(d.name, !d.auto_update),
    { success: d.auto_update ? 'Авто-обновление выключено' : 'Авто-обновление включено' },
  )
  if (ok !== undefined)
    await load()
}

// редкие действия — в «⋯»-меню (design/05 §3): inline остаются только
// запуск и пауза
const menuItems = computed<DropdownMenuItem[][]>(() => {
  const d = dag.value
  if (!d)
    return []
  const main: DropdownMenuItem[] = []
  if (canManage.value) {
    main.push({ label: 'Расписание…', icon: 'i-lucide-alarm-clock', onSelect: () => { scheduleTarget.value = d } })
    if (d.schedule)
      main.push({ label: 'Backfill за период…', icon: 'i-lucide-calendar-clock', onSelect: () => { backfillTarget.value = d } })
  }
  if (isAdmin.value) {
    main.push({
      label: 'Обновить из registry',
      icon: 'i-lucide-cloud-download',
      disabled: isUpdating.value,
      onSelect: () => sync(),
    })
    main.push({
      label: d.auto_update ? 'Выключить авто-обновление' : 'Включить авто-обновление',
      icon: 'i-lucide-refresh-ccw-dot',
      onSelect: () => toggleAutoUpdate(),
    })
  }

  const groups: DropdownMenuItem[][] = []
  if (main.length > 0)
    groups.push(main)
  if (isAdmin.value)
    groups.push([{ label: 'Удалить даг…', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { deleteTarget.value = d } }])
  return groups
})

// ── таблицы табы «Схема» ────────────────────────────────

function formatSeconds(sec: number): string {
  if (!sec)
    return '—'
  if (sec % 3600 === 0)
    return `${sec / 3600}ч`
  if (sec % 60 === 0)
    return `${sec / 60}м`
  return `${sec}с`
}

// эффективные ресурсы: непустое поле оверрайда из админки перекрывает
// значение манифеста
function effectiveResources(t: DagTask) {
  const r = t.resources
  const o = overrideByTask.value.get(t.name)
  if (!o)
    return r
  return {
    cpu_request: o.cpu_request || r?.cpu_request || '',
    cpu_limit: o.cpu_limit || r?.cpu_limit || '',
    memory_request: o.memory_request || r?.memory_request || '',
    memory_limit: o.memory_limit || r?.memory_limit || '',
  }
}

function formatResources(t: DagTask): string {
  const r = effectiveResources(t)
  if (!r)
    return '—'
  const parts: string[] = []
  if (r.cpu_request || r.cpu_limit)
    parts.push(`cpu ${r.cpu_request || '—'}/${r.cpu_limit || '—'}`)
  if (r.memory_request || r.memory_limit)
    parts.push(`mem ${r.memory_request || '—'}/${r.memory_limit || '—'}`)
  return parts.length ? parts.join(' · ') : '—'
}

const taskColumns: TableColumn<DagTask>[] = [
  { accessorKey: 'name', header: 'Таск' },
  { id: 'depends_on', header: 'Зависимости' },
  { accessorKey: 'retries', header: 'Ретраи' },
  { id: 'timeout', header: 'Таймаут' },
  { id: 'resources', header: 'Ресурсы (req/lim)' },
  { accessorKey: 'priority', header: 'Приоритет' },
  { id: 'injections', header: 'Инъекции' },
]

// таба «Настройки»: эффективные ресурсы тасков + источник
const resourceColumns: TableColumn<DagTask>[] = [
  { accessorKey: 'name', header: 'Таск' },
  { id: 'cpu', header: 'CPU (req/lim)' },
  { id: 'memory', header: 'Память (req/lim)' },
  { id: 'source', header: 'Источник' },
  { id: 'actions', header: '' },
]

const regColumns: TableColumn<DagRegistration>[] = [
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'source', header: 'Источник' },
  { accessorKey: 'image', header: 'Образ' },
  { accessorKey: 'created_at', header: 'Создана' },
  { accessorKey: 'finished_at', header: 'Завершена' },
]
</script>

<template>
  <UDashboardPanel id="dag-details">
    <template #header>
      <UDashboardNavbar :title="dagName">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/dags" aria-label="К списку дагов" />
        </template>
        <template #right>
          <UBadge v-if="dag?.paused" color="warning" variant="subtle" size="lg">пауза</UBadge>
          <UBadge v-if="isUpdating" color="info" variant="subtle" size="lg">
            <UIcon name="i-lucide-loader-circle" class="animate-spin" />
            обновляется
          </UBadge>
          <UButton
            v-if="canManage"
            icon="i-lucide-play"
            label="Запустить"
            color="primary"
            variant="soft"
            @click="triggerTarget = dag"
          />
          <UTooltip v-if="canManage" :text="dag?.paused ? 'Снять с паузы' : 'Поставить на паузу'">
            <UButton
              :icon="dag?.paused ? 'i-lucide-play-circle' : 'i-lucide-pause-circle'"
              color="warning"
              variant="ghost"
              :aria-label="dag?.paused ? 'Снять с паузы' : 'Поставить на паузу'"
              @click="togglePaused"
            />
          </UTooltip>
          <RowMenu v-if="menuItems.length" :items="menuItems" size="md" />
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить"
            @click="load(); loadRuns()"
          />
        </template>
      </UDashboardNavbar>
      <UDashboardToolbar>
        <template #left>
          <UTabs
            v-model="tab"
            :items="tabItems"
            :content="false"
            color="neutral"
            variant="pill"
            size="sm"
          />
        </template>
      </UDashboardToolbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки дага"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <template v-if="dag">
        <!-- ── Обзор: как даг себя ведёт ── -->
        <template v-if="tab === 'overview'">
          <MetaGrid>
            <MetaItem label="Расписание">
              <template v-if="dag.schedule">
                <span class="font-mono">{{ dag.schedule }}</span>
                <UBadge v-if="dag.catchup" color="info" variant="subtle" size="sm" class="ml-1.5">catchup</UBadge>
              </template>
              <span v-else class="text-muted">— (запуск вручную)</span>
            </MetaItem>
            <MetaItem label="Следующий запуск">
              <template v-if="dag.next_run_at && !dag.paused">
                <RelativeTime :time="dag.next_run_at" />
              </template>
            </MetaItem>
            <MetaItem label="Лимит активных ранов">{{ dag.max_active_runs || 'без лимита' }}</MetaItem>
            <MetaItem label="Пул слотов">
              <span class="font-mono">{{ dag.pool || 'default' }}</span>
            </MetaItem>
            <MetaItem label="Переменные и секреты">
              <NuxtLink :to="`/env?dag_name=${encodeURIComponent(dag.name)}`" class="text-primary hover:underline">
                env дага →
              </NuxtLink>
            </MetaItem>
            <MetaItem label="Образ" span>
              <CopyText :text="dag.image" mono />
            </MetaItem>
          </MetaGrid>

          <section>
            <SectionHeader title="Последние раны" :count="runs.length">
              <div class="flex items-center gap-3">
                <DagRunSpark v-if="runs.length > 1" :runs="runs" />
                <UButton
                  :to="`/runs?dag_name=${encodeURIComponent(dag.name)}`"
                  label="Все раны"
                  trailing-icon="i-lucide-arrow-right"
                  color="neutral"
                  variant="ghost"
                  size="xs"
                />
              </div>
            </SectionHeader>
            <UAlert v-if="runsError" color="error" variant="subtle" :title="runsError" />
            <UTable
              v-else
              :data="runs"
              :columns="runColumns"
              :ui="{ ...denseTableUi, tr: 'cursor-pointer' }"
              @select="openRun"
            >
              <template #status-cell="{ row }">
                <StatusBadge kind="run" :status="row.original.status" size="sm" />
              </template>
              <template #logical_date-cell="{ row }">
                <span class="whitespace-nowrap">{{ formatDateShort(row.original.logical_date) }}</span>
              </template>
              <template #started-cell="{ row }">
                <div class="flex items-center gap-1.5">
                  <RelativeTime :time="row.original.created_at" />
                  <UBadge :color="runTriggerColor(row.original.trigger)" variant="subtle" size="sm">
                    {{ runTriggerLabel(row.original.trigger) }}
                  </UBadge>
                </div>
              </template>
              <template #duration-cell="{ row }">
                <span class="whitespace-nowrap tabular-nums">
                  {{ formatDuration(row.original.created_at, row.original.finished_at, row.original.status === 'running' ? nowTick : undefined) }}
                </span>
              </template>
              <template #empty>
                <EmptyState icon="i-lucide-list" title="Ранов ещё не было">
                  <UButton v-if="canManage" size="sm" icon="i-lucide-play" label="Запустить" @click="triggerTarget = dag" />
                </EmptyState>
              </template>
            </UTable>
          </section>

          <section v-if="taskStats.length">
            <SectionHeader title="Таски по ресурсам" />
            <UTable :data="taskStats" :columns="statColumns" :ui="denseTableUi">
              <template #task-cell="{ row }">
                <span class="font-medium">{{ row.original.task }}</span>
              </template>
              <template #duration-cell="{ row }">
                <span class="tabular-nums">
                  {{ formatSeconds(Math.round(row.original.avg_duration_sec)) }}
                  <span class="text-muted">/ {{ formatSeconds(Math.round(row.original.max_duration_sec)) }}</span>
                </span>
              </template>
              <template #memory-cell="{ row }">
                <template v-if="row.original.max_peak_memory_bytes !== undefined">
                  {{ formatBytes(row.original.avg_peak_memory_bytes) }}
                  <span class="text-muted">/ {{ formatBytes(row.original.max_peak_memory_bytes) }}</span>
                </template>
                <span v-else class="text-muted">—</span>
              </template>
              <template #limit-cell="{ row }">
                <div class="flex items-center gap-1.5">
                  <span class="font-mono text-xs">{{ memoryLimitByTask.get(row.original.task)?.value ?? '—' }}</span>
                  <UTooltip v-if="memoryLimitByTask.get(row.original.task)?.overridden" text="задан в админке; значение из кода — рекомендуемое">
                    <UBadge color="info" variant="subtle" size="sm">админка</UBadge>
                  </UTooltip>
                  <UTooltip v-if="canManage" text="Изменить лимиты таска">
                    <UButton
                      icon="i-lucide-pencil" size="xs" color="neutral" variant="ghost"
                      aria-label="Изменить лимиты таска" @click="openResources(row.original.task)"
                    />
                  </UTooltip>
                </div>
              </template>
            </UTable>
            <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
              <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
              Агрегат за последние {{ statsRuns }} завершённых ранов; память приблизительна
              (семплы executor'а) и может отсутствовать у коротких попыток.
            </p>
          </section>
        </template>

        <!-- ── Схема: как даг устроен ── -->
        <template v-else-if="tab === 'schema'">
          <section>
            <SectionHeader title="Граф" />
            <RunDagGraph :manifest-tasks="dag.tasks" />
          </section>

          <section>
            <SectionHeader title="Таски" :count="dag.tasks.length" />
            <UTable :data="dag.tasks" :columns="taskColumns" :ui="denseTableUi">
              <template #name-cell="{ row }">
                <span class="font-medium">{{ row.original.name }}</span>
              </template>
              <template #depends_on-cell="{ row }">
                <div v-if="row.original.depends_on.length" class="flex flex-wrap gap-1">
                  <UBadge
                    v-for="dep in row.original.depends_on"
                    :key="dep.task"
                    color="neutral"
                    variant="subtle"
                    size="sm"
                  >
                    {{ dep.task }}{{ dep.streamed ? ' (stream)' : '' }}
                  </UBadge>
                </div>
                <span v-else class="text-muted">—</span>
              </template>
              <template #retries-cell="{ row }">
                <template v-if="row.original.retries">
                  {{ row.original.retries }}
                  <span v-if="row.original.retry_delay_sec" class="text-xs text-muted">
                    · пауза {{ formatSeconds(row.original.retry_delay_sec) }}
                  </span>
                </template>
                <span v-else class="text-muted">—</span>
              </template>
              <template #timeout-cell="{ row }">
                {{ formatSeconds(row.original.timeout_sec) }}
              </template>
              <template #resources-cell="{ row }">
                <div class="flex items-center gap-1.5">
                  <span class="font-mono text-xs">{{ formatResources(row.original) }}</span>
                  <UTooltip v-if="overrideByTask.has(row.original.name)" text="лимиты заданы в админке; значения из кода — рекомендуемые">
                    <UBadge color="info" variant="subtle" size="sm">админка</UBadge>
                  </UTooltip>
                  <UTooltip v-if="canManage" text="Изменить лимиты таска">
                    <UButton
                      icon="i-lucide-pencil" size="xs" color="neutral" variant="ghost"
                      aria-label="Изменить лимиты таска" @click="openResources(row.original.name)"
                    />
                  </UTooltip>
                </div>
              </template>
              <template #priority-cell="{ row }">
                {{ row.original.priority || '—' }}
              </template>
              <template #injections-cell="{ row }">
                <div v-if="row.original.secrets?.length || row.original.variables?.length" class="flex flex-wrap gap-1">
                  <UTooltip v-for="s in row.original.secrets" :key="`s-${s.env}`" :text="`env ${s.env}`">
                    <UBadge color="warning" variant="subtle" size="sm" class="font-mono">
                      <UIcon name="i-lucide-key-round" class="size-3" />
                      {{ s.secret }}
                    </UBadge>
                  </UTooltip>
                  <UTooltip v-for="v in row.original.variables" :key="`v-${v.env}`" :text="`env ${v.env}`">
                    <UBadge color="neutral" variant="subtle" size="sm" class="font-mono">
                      <UIcon name="i-lucide-variable" class="size-3" />
                      {{ v.variable }}
                    </UBadge>
                  </UTooltip>
                </div>
                <span v-else class="text-muted">—</span>
              </template>
            </UTable>
          </section>

          <section v-if="registrations.length">
            <SectionHeader title="История регистраций" :count="registrations.length" />
            <UTable :data="registrations" :columns="regColumns" :ui="denseTableUi">
              <template #status-cell="{ row }">
                <div class="flex flex-col gap-0.5">
                  <StatusBadge kind="registration" :status="row.original.status" size="sm" class="w-fit" />
                  <span v-if="row.original.error" class="text-xs text-error">{{ row.original.error }}</span>
                </div>
              </template>
              <template #source-cell="{ row }">
                {{ row.original.source === 'auto' ? 'авто (digest)' : 'вручную' }}
              </template>
              <template #image-cell="{ row }">
                <span class="font-mono text-xs">{{ row.original.image }}</span>
              </template>
              <template #created_at-cell="{ row }">
                <RelativeTime :time="row.original.created_at" />
              </template>
              <template #finished_at-cell="{ row }">
                <RelativeTime :time="row.original.finished_at" />
              </template>
            </UTable>
          </section>

          <MetaGrid>
            <MetaItem label="SDK">{{ dag.sdk_version }}</MetaItem>
            <MetaItem label="Зарегистрирован">{{ formatDateTime(dag.created_at) }}</MetaItem>
            <MetaItem label="Обновлён">{{ formatDateTime(dag.modified_at) }}</MetaItem>
            <MetaItem label="Digest" span>
              <CopyText :text="dag.image_digest" mono />
            </MetaItem>
          </MetaGrid>
        </template>

        <!-- ── Настройки: хранение, лимиты, ресурсы тасков ── -->
        <template v-else>
          <DagPoolCard
            :dag-name="dagName"
            :pool="dag.pool"
            :can-manage="canManage"
            @saved="load()"
          />

          <DagSettingsCard :dag-name="dagName" :can-manage="canManage" />

          <section>
            <SectionHeader title="Ресурсы тасков" />
            <UTable :data="dag.tasks" :columns="resourceColumns" :ui="denseTableUi">
              <template #name-cell="{ row }">
                <span class="font-medium">{{ row.original.name }}</span>
              </template>
              <template #cpu-cell="{ row }">
                <span class="font-mono text-xs">
                  {{ effectiveResources(row.original)?.cpu_request || '—' }} /
                  {{ effectiveResources(row.original)?.cpu_limit || '—' }}
                </span>
              </template>
              <template #memory-cell="{ row }">
                <span class="font-mono text-xs">
                  {{ effectiveResources(row.original)?.memory_request || '—' }} /
                  {{ effectiveResources(row.original)?.memory_limit || '—' }}
                </span>
              </template>
              <template #source-cell="{ row }">
                <UBadge v-if="overrideByTask.has(row.original.name)" color="info" variant="subtle" size="sm">
                  админка
                </UBadge>
                <span v-else class="text-xs text-muted">из кода</span>
              </template>
              <template #actions-cell="{ row }">
                <div class="flex justify-end">
                  <UTooltip v-if="canManage" text="Изменить лимиты таска">
                    <UButton
                      icon="i-lucide-pencil" size="xs" color="neutral" variant="ghost"
                      aria-label="Изменить лимиты таска" @click="openResources(row.original.name)"
                    />
                  </UTooltip>
                </div>
              </template>
            </UTable>
            <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
              <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
              Показаны эффективные значения: непустое поле из админки перекрывает значение из
              кода дага и применяется со следующего запуска попытки.
            </p>
          </section>
        </template>
      </template>

      <DagTaskResourcesModal
        :dag-name="dagName"
        :task="resourcesTarget"
        :override="resourcesTarget ? (overrideByTask.get(resourcesTarget.name) ?? null) : null"
        @close="resourcesTarget = null"
        @saved="loadOverrides"
      />
      <DagTriggerModal :dag="triggerTarget" @close="triggerTarget = null" />
      <DagBackfillModal :dag="backfillTarget" @close="backfillTarget = null" />
      <DagScheduleModal :dag="scheduleTarget" @close="scheduleTarget = null" @saved="onScheduleSaved" />
      <DagDeleteModal :dag="deleteTarget" @close="deleteTarget = null" @deleted="onDeleted" />
    </template>
  </UDashboardPanel>
</template>
