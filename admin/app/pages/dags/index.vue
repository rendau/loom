<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags, setDagPaused } from '~/api/dag.api'
import { listProjectRegistrations, listProjects } from '~/api/project.api'
import type { Dag } from '~/types/dag'
import type { Project, ProjectRegistration } from '~/types/project'

// Список дагов-инстансов: узкая таблица-обзор парка (проект+имя, бейджи,
// расписание, таски, обновлён) — образ/digest/SDK живут в карточке дага и
// проекта. Регистрация образов — на странице проектов: даг здесь только
// заводится от уже зарегистрированного шаблона.
// Частые действия (запуск, пауза) — inline, остальные — в «⋯»-меню.

const { isAdmin, canManageDag } = useAuth()

const route = useRoute()

const PAGE_SIZE = 100

const dags = ref<Dag[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

// даг требует переменных, значения которых ещё не заведены: до первого
// запуска это больше нигде не видно
const envGaps = useDagEnvGaps()

// фильтр по проекту: приходит из ссылки карточки проекта (?project=…)
// reka-ui Select не принимает '' как value — «все проекты» через sentinel
const ALL_PROJECTS = '__all__'

const projectFilter = ref(String(route.query.project ?? '') || ALL_PROJECTS)
const projects = ref<Project[]>([])

const projectItems = computed(() => [
  { label: 'Все проекты', value: ALL_PROJECTS },
  ...projects.value.map(p => ({ label: p.name, value: p.name })),
])

async function load() {
  loading.value = true
  try {
    const rep = await listDags({
      list_params: {
        page: page.value - 1,
        page_size: PAGE_SIZE,
        with_total_count: true,
        sort: ['project_name', 'name'],
      },
      project: projectFilter.value === ALL_PROJECTS ? undefined : projectFilter.value,
    })
    dags.value = rep.results
    totalCount.value = Number(rep.pagination_info.total_count)
    loadError.value = ''
    // незаполненные переменные/секреты — по уже загруженным дагам
    await envGaps.load(dags.value)
  }
  catch (error) {
    // ошибка загрузки — inline alert (тост исчезает, а страница остаётся пустой)
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

watch(page, load)

watch(projectFilter, async (v) => {
  page.value = 1
  await navigateTo({ query: v === ALL_PROJECTS ? {} : { project: v } })
  await load()
})

// ── регистрации: индикация «проект обновляется» ────────────────────────
// Панель статусов и ошибок живёт на странице проектов — здесь достаточно
// бейджа у дагов того проекта, чей образ сейчас перерегистрируется.

const registrations = ref<ProjectRegistration[]>([])
let regTimer: ReturnType<typeof setInterval> | undefined

const activeRegistrations = computed(() =>
  registrations.value.filter(r => r.status === 'pending' || r.status === 'running'))

function isUpdating(dag: Dag): boolean {
  return activeRegistrations.value.some(r => r.project_name === dag.project)
}

async function loadRegistrations() {
  registrations.value = (await listProjectRegistrations({ active: true, limit: 50 })).results ?? []
}

function ensureRegPolling() {
  if (regTimer)
    return
  regTimer = setInterval(async () => {
    const wasActive = activeRegistrations.value.length
    await loadRegistrations()
    // регистрация доехала — манифесты дагов могли обновиться
    if (wasActive > 0 && activeRegistrations.value.length < wasActive)
      await load()
    if (activeRegistrations.value.length === 0)
      stopRegPolling()
  }, 3000)
}

function stopRegPolling() {
  if (regTimer) {
    clearInterval(regTimer)
    regTimer = undefined
  }
}

async function loadProjects() {
  try {
    projects.value = (await listProjects({ list_params: { page_size: 200, sort: ['name'] } })).results ?? []
  }
  catch {
    // фильтр останется с одним «все проекты» — список дагов всё равно виден
  }
}

onMounted(async () => {
  await Promise.all([load(), loadProjects()])
  await loadRegistrations()
  if (activeRegistrations.value.length > 0)
    ensureRegPolling()
})

onUnmounted(stopRegPolling)

// модалки над дагом — общие компоненты (используются и карточкой дага)
const triggerTarget = ref<Dag | null>(null)
const backfillTarget = ref<Dag | null>(null)
const deleteTarget = ref<Dag | null>(null)

async function onDeleted() {
  deleteTarget.value = null
  await load()
}

// расписание дага (cron + catchup) правится через модалку
const scheduleTarget = ref<Dag | null>(null)

async function onScheduleSaved() {
  scheduleTarget.value = null
  await load()
}

async function togglePaused(dag: Dag) {
  const ok = await action.run(
    () => setDagPaused(dag, !dag.paused),
    { success: dag.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
  )
  if (ok !== undefined)
    await load()
}

// редкие действия — в «⋯»-меню строки; состав по правам (design/05:
// недоступные действия не рендерятся)
function menuItems(dag: Dag): DropdownMenuItem[][] {
  const main: DropdownMenuItem[] = []
  if (canManageDag(dag)) {
    main.push({ label: 'Расписание…', icon: 'i-lucide-alarm-clock', onSelect: () => { scheduleTarget.value = dag } })
    if (dag.schedule)
      main.push({ label: 'Backfill за период…', icon: 'i-lucide-calendar-clock', onSelect: () => { backfillTarget.value = dag } })
  }
  // образ и его обновление — свойства проекта, они на его странице
  main.push({
    label: 'Проект дага',
    icon: 'i-lucide-package',
    onSelect: () => navigateTo(`/projects/${encodeURIComponent(dag.project)}`),
  })

  const groups: DropdownMenuItem[][] = [main]
  if (isAdmin.value)
    groups.push([{ label: 'Удалить…', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { deleteTarget.value = dag } }])
  return groups
}

function openDag(_e: Event, row: TableRow<Dag>) {
  navigateTo(dagLink(row.original))
}

// статус-стрип: цвет квадратика последнего рана
function lastRunClass(status: string): string {
  switch (status) {
    case 'success': return 'bg-success'
    case 'failed': return 'bg-error'
    case 'running': return 'bg-info animate-pulse'
    default: return 'bg-accented'
  }
}

const columns: TableColumn<Dag>[] = [
  { accessorKey: 'name', header: 'Даг' },
  { id: 'project', header: 'Проект' },
  { id: 'last_runs', header: 'Последние раны' },
  { accessorKey: 'schedule', header: 'Расписание' },
  { id: 'tasks', header: 'Тасков' },
  { id: 'modified', header: 'Обновлён' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="dags">
    <template #header>
      <UDashboardNavbar title="Даги">
        <template #right>
          <USelectMenu
            v-model="projectFilter"
            :items="projectItems"
            value-key="value"
            class="w-48"
            size="sm"
          />
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить список"
            @click="load"
          />
          <UButton
            v-if="isAdmin"
            icon="i-lucide-package"
            color="neutral"
            variant="subtle"
            label="Проекты"
            to="/projects"
          />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки дагов"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <UTable
        :data="dags"
        :columns="columns"
        :loading="loading"
        :ui="{ ...denseTableUi, tr: 'cursor-pointer' }"
        @select="openDag"
      >
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-highlighted">{{ row.original.name }}</span>
            <UTooltip v-if="envGaps.missing(row.original)" text="Не заполнены переменные или секреты — запуск таска упадёт launch_failed">
              <UBadge color="error" variant="subtle" size="sm">
                <UIcon name="i-lucide-triangle-alert" class="size-3" />
                env: {{ envGaps.missing(row.original) }}
              </UBadge>
            </UTooltip>
            <UBadge v-if="row.original.paused" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
            <UBadge v-if="isUpdating(row.original)" color="info" variant="subtle" size="sm">
              <UIcon name="i-lucide-loader-circle" class="animate-spin" />
              обновляется
            </UBadge>
            <UTooltip v-if="row.original.template_orphaned" text="Шаблон пропал из образа при последней регистрации — даг работает на последнем известном манифесте">
              <UBadge color="warning" variant="subtle" size="sm">шаблон исчез</UBadge>
            </UTooltip>
          </div>
        </template>

        <template #project-cell="{ row }">
          <div class="min-w-0">
            <NuxtLink
              :to="`/projects/${encodeURIComponent(row.original.project)}`"
              class="font-medium text-highlighted hover:text-primary hover:underline"
              @click.stop
            >
              {{ row.original.project }}
            </NuxtLink>
            <!-- имя дага в образе: у нескольких инстансов одного шаблона
                 различаются только имена, шаблон общий -->
            <div class="font-mono text-[11px]/4 text-dimmed">
              {{ row.original.template }}
            </div>
          </div>
        </template>

        <template #last_runs-cell="{ row }">
          <!-- старые слева, свежий справа; клик — в ран -->
          <div v-if="row.original.last_runs?.length" class="flex items-center gap-1">
            <UTooltip
              v-for="lr in [...row.original.last_runs].reverse()"
              :key="lr.run_id"
              :text="`${runStatusLabel(lr.status as never)} · ${lr.run_id}`"
            >
              <NuxtLink
                :to="`/runs/${encodeURIComponent(lr.run_id)}`"
                class="block size-2.5 rounded-[3px]"
                :class="lastRunClass(lr.status)"
                :aria-label="`Ран ${lr.run_id}`"
              />
            </UTooltip>
          </div>
          <span v-else class="text-muted">—</span>
        </template>

        <template #schedule-cell="{ row }">
          <div v-if="row.original.schedule">
            <div class="flex items-center gap-1.5">
              <span class="font-mono">{{ row.original.schedule }}</span>
              <UBadge v-if="row.original.catchup" color="info" variant="subtle" size="sm">catchup</UBadge>
            </div>
            <div v-if="!row.original.paused" class="text-xs text-muted">
              след.: <RelativeTime :time="row.original.next_run_at" />
            </div>
          </div>
          <span v-else class="text-muted">—</span>
        </template>

        <template #tasks-cell="{ row }">
          {{ row.original.tasks.length }}
        </template>

        <template #modified-cell="{ row }">
          <RelativeTime :time="row.original.modified_at ?? row.original.created_at" />
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip v-if="canManageDag(row.original)" text="Запустить ран">
              <UButton
                icon="i-lucide-play"
                size="sm"
                color="primary"
                variant="ghost"
                aria-label="Запустить ран"
                @click="triggerTarget = row.original"
              />
            </UTooltip>
            <UTooltip v-if="canManageDag(row.original)" :text="row.original.paused ? 'Снять с паузы' : 'Поставить на паузу'">
              <UButton
                :icon="row.original.paused ? 'i-lucide-play-circle' : 'i-lucide-pause-circle'"
                size="sm"
                color="warning"
                variant="ghost"
                :aria-label="row.original.paused ? 'Снять с паузы' : 'Поставить на паузу'"
                @click="togglePaused(row.original)"
              />
            </UTooltip>
            <RowMenu v-if="menuItems(row.original).length" :items="menuItems(row.original)" />
          </div>
        </template>

        <template #empty>
          <!-- при ошибке загрузки пустота — не «дагов нет», причина в алерте выше -->
          <div v-if="loadError" class="py-6" />
          <EmptyState
            v-else
            icon="i-lucide-workflow"
            title="Дагов пока нет"
            description="Даги заводятся от дагов образа: зарегистрируйте проект — его даги появятся сразу после describe."
          >
            <UButton v-if="isAdmin" size="sm" icon="i-lucide-package" label="К проектам" to="/projects" />
          </EmptyState>
        </template>
      </UTable>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-end border-t border-default p-2">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" size="sm" />
      </div>

      <DagTriggerModal :dag="triggerTarget" @close="triggerTarget = null" />
      <DagBackfillModal :dag="backfillTarget" @close="backfillTarget = null" />
      <DagScheduleModal :dag="scheduleTarget" @close="scheduleTarget = null" @saved="onScheduleSaved" />
      <DagDeleteModal :dag="deleteTarget" @close="deleteTarget = null" @deleted="onDeleted" />
    </template>
  </UDashboardPanel>
</template>
