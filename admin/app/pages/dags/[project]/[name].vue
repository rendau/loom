<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow, TabsItem } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { getDag, getDagStats, listTaskResources, setDagPaused } from '~/api/dag.api'
import { listProjectRegistrations } from '~/api/project.api'
import { listRuns, runDagQuery } from '~/api/run.api'
import type { Dag, DagTask, DagTaskStat, TaskResourcesOverride } from '~/types/dag'
import type { ProjectRegistration } from '~/types/project'
import type { Run } from '~/types/run'

// Карточка дага — два лица (design/02): таба «Обзор» — как даг себя ведёт
// (последние раны, таски по ресурсам), таба «Схема» — как устроен (граф,
// манифест, история регистраций, служебное). Обзор — дефолт: поведение
// смотрят чаще устройства.

const route = useRoute()
const router = useRouter()
// идентификатор дага составной: проект (образ) + имя инстанса
const dagRef = { project: String(route.params.project), name: String(route.params.name) }

const { isAdmin, canManageDag } = useAuth()
const canManage = computed(() => canManageDag(dagRef))

// проект в пути — ссылка на его карточку: имя дага уникально только
// внутри проекта, и «из какого он образа» — первое, что спрашивают
const crumbs = [
  { label: 'Даги', icon: 'i-lucide-workflow', to: '/dags' },
  { label: dagRef.project, to: `/projects/${encodeURIComponent(dagRef.project)}` },
  { label: dagRef.name },
]

const dag = ref<Dag | null>(null)
const registrations = ref<ProjectRegistration[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

const isUpdating = computed(() =>
  registrations.value.some(r => r.status === 'pending' || r.status === 'running'))

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    dag.value = await getDag(dagRef)
    // регистрации ведутся на проект: его образ несёт манифест этого дага
    registrations.value = (await listProjectRegistrations({
      project_name: dagRef.project,
      limit: 20,
    })).results ?? []
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

// ── переменные и секреты дага (таба «Env») ──────────────

// состав — из манифеста, заполненность — из записей админки; счётчик
// «не заполнено» нужен на бейдже табы до её открытия, поэтому грузим
// здесь, а не в самой карточке
const dagTasks = computed(() => dag.value?.tasks ?? [])
const dagEnv = useDagEnvRequirements(computed(() => dagRef), dagTasks)

// ── табы (?tab=): overview — дефолт ─────────────────────

const knownTabs = ['overview', 'schema', 'env', 'settings']
const tab = ref(knownTabs.includes(String(route.query.tab)) ? String(route.query.tab) : 'overview')

// бейдж табы — число незаполненных: даг с пустой переменной упадёт
// launch_failed, и это надо видеть, не открывая табу
const tabItems = computed<TabsItem[]>(() => [
  { label: 'Обзор', value: 'overview', icon: 'i-lucide-activity' },
  { label: 'Схема', value: 'schema', icon: 'i-lucide-workflow' },
  {
    label: 'Env',
    value: 'env',
    icon: 'i-lucide-key-round',
    badge: dagEnv.missing.value > 0
      ? { label: String(dagEnv.missing.value), color: 'error' as const, variant: 'subtle' as const }
      : undefined,
  },
  { label: 'Настройки', value: 'settings', icon: 'i-lucide-settings-2' },
])

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
      ...runDagQuery(dagRef),
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
    const rep = await getDagStats(dagRef)
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
    resourceOverrides.value = (await listTaskResources(dagRef)).results ?? []
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
  await Promise.all([load(), loadRuns(), loadStats(), loadOverrides(), dagEnv.load()])
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
    () => setDagPaused(d, !d.paused),
    { success: d.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
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
  // образ и его обновление — свойства проекта: они на его странице
  main.push({
    label: 'Проект дага',
    icon: 'i-lucide-package',
    onSelect: () => navigateTo(`/projects/${encodeURIComponent(d.project)}`),
  })

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

// таба «Настройки»: эффективные ресурсы тасков + источник
const resourceColumns: TableColumn<DagTask>[] = [
  { accessorKey: 'name', header: 'Таск' },
  { id: 'cpu', header: 'CPU (req/lim)' },
  { id: 'memory', header: 'Память (req/lim)' },
  { id: 'source', header: 'Источник' },
  { id: 'actions', header: '' },
]

const regColumns: TableColumn<ProjectRegistration>[] = [
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
      <UDashboardNavbar :title="dagRef.name">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/dags" aria-label="К списку дагов" />
        </template>

        <template #title>
          <PageCrumbs :items="crumbs" kind="даг" />
        </template>
        <template #right>
          <UBadge v-if="dag?.paused" color="warning" variant="subtle" size="lg">пауза</UBadge>
          <UTooltip v-if="dagEnv.missing.value > 0" text="Запуск таска упадёт launch_failed — заполните значения">
            <UBadge
              color="error"
              variant="subtle"
              size="lg"
              class="cursor-pointer"
              @click="tab = 'env'"
            >
              <UIcon name="i-lucide-triangle-alert" />
              не заполнено: {{ dagEnv.missing.value }}
            </UBadge>
          </UTooltip>
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
            <MetaItem label="Проект">
              <!-- откуда даг: образ и его каталог живут в карточке проекта -->
              <NuxtLink
                :to="`/projects/${encodeURIComponent(dagRef.project)}`"
                class="font-mono text-primary hover:underline"
              >
                {{ dagRef.project }}
              </NuxtLink>
            </MetaItem>
            <MetaItem label="Шаблон">
              <!-- манифест живёт на шаблоне: граф, таски и требования к
                   окружению — на его странице -->
              <NuxtLink :to="templateLink(dagRef.project, dag.template)" class="font-mono text-primary hover:underline">
                {{ dag.template }}
              </NuxtLink>
              <UBadge v-if="dag.template_orphaned" color="warning" variant="subtle" size="sm" class="ml-1.5">
                исчез из образа
              </UBadge>
            </MetaItem>
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
              <NuxtLink :to="`/env?scope=${encodeURIComponent(dagRefLabel(dag))}`" class="text-primary hover:underline">
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
                  :to="`/runs?dag=${encodeURIComponent(dagRefLabel(dag))}`"
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
            <DagTaskTable
              :tasks="dag.tasks"
              :overrides="overrideByTask"
              :can-manage="canManage"
              @edit-resources="openResources"
            />
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

        <!-- ── Env: что даг требует от окружения ── -->
        <template v-else-if="tab === 'env'">
          <DagEnvCard
            :dag-ref="dagRef"
            :requirements="dagEnv.requirements.value"
            :loading="dagEnv.loading.value"
            :load-error="dagEnv.loadError.value"
            @reload="dagEnv.load()"
          />
        </template>

        <!-- ── Настройки: хранение, лимиты, ресурсы тасков ── -->
        <template v-else>
          <DagPoolCard
            :dag-ref="dagRef"
            :pool="dag.pool"
            :can-manage="canManage"
            @saved="load()"
          />

          <DagSettingsCard :dag-ref="dagRef" :can-manage="canManage" />

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
        :dag-ref="dagRef"
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
