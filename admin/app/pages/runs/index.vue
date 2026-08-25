<script setup lang="ts">
import type { TableColumn, TableRow, TabsItem } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import { cancelRun, countRuns, listRuns } from '~/api/run.api'
import type { Run, RunCount } from '~/types/run'

// Живой список ранов: авто-поллинг, фильтры (даг — селект, статус — чипы)
// в URL — срез можно переслать ссылкой. Колонки — только для решения
// «куда смотреть»: статус, даг, дата данных, запущен, длительность.

const PAGE_SIZE = 30

const runs = ref<Run[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)
const loadError = ref('')

// фильтры инициализируются из query (?dag_name=…&status=…) — так сюда
// ведут ссылки с карточки дага и дашборда
const route = useRoute()
const router = useRouter()

// reka-ui Select не принимает '' как value — «все даги» через sentinel
const ALL_DAGS = '__all__'
const ALL_STATUSES = 'all'

const dagFilter = ref<string>(
  typeof route.query.dag_name === 'string' && route.query.dag_name ? route.query.dag_name : ALL_DAGS,
)
const statusFilter = ref<string>(
  typeof route.query.status === 'string' && route.query.status ? route.query.status : ALL_STATUSES,
)

// счётчики в чипах — в рамках выбранного дага
const counts = ref<RunCount | null>(null)

async function loadCounts() {
  try {
    counts.value = await countRuns(dagFilter.value === ALL_DAGS ? undefined : dagFilter.value)
  }
  catch {
    counts.value = null // чипы просто останутся без счётчиков
  }
}

const statusItems = computed<TabsItem[]>(() => {
  const badge = (n?: string) => (counts.value && Number(n) > 0 ? Number(n) : undefined)
  const c = counts.value
  return [
    { label: 'Все', value: ALL_STATUSES },
    { label: 'Выполняются', value: 'running', badge: badge(c?.running) },
    { label: 'Провалы', value: 'failed', badge: badge(c?.failed) },
    { label: 'Успех', value: 'success', badge: badge(c?.success) },
    { label: 'Остановлены', value: 'canceled', badge: badge(c?.canceled) },
  ]
})

const dagNames = ref<string[]>([])
const dagItems = computed(() => [
  { label: 'Все даги', value: ALL_DAGS },
  ...dagNames.value.map(n => ({ label: n, value: n })),
])

async function loadDagNames() {
  try {
    const rep = await listDags({ list_params: { page_size: 500, sort: ['name'] } })
    dagNames.value = rep.results.map(d => d.name)
  }
  catch {
    // селект просто останется без вариантов — фильтр по URL всё ещё работает
  }
}

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    const rep = await listRuns({
      list_params: {
        page: page.value - 1,
        page_size: PAGE_SIZE,
        with_total_count: true,
        sort: ['-created_at'],
      },
      dag_name: dagFilter.value === ALL_DAGS ? undefined : dagFilter.value,
      status: statusFilter.value === ALL_STATUSES ? undefined : statusFilter.value,
    })
    runs.value = rep.results
    totalCount.value = Number(rep.pagination_info.total_count)
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

onMounted(async () => {
  await Promise.all([load(), loadDagNames(), loadCounts()])
})
watch(page, () => load())
watch([dagFilter, statusFilter], () => {
  page.value = 1
  load()
})
watch(dagFilter, loadCounts)

// фильтры живут в URL — ссылку на срез («провалы дага X») можно передать
watch([dagFilter, statusFilter], () => {
  const query: Record<string, string> = {}
  if (dagFilter.value !== ALL_DAGS)
    query.dag_name = dagFilter.value
  if (statusFilter.value !== ALL_STATUSES)
    query.status = statusFilter.value
  router.replace({ query })
})

// список — живой: фоновый рефреш без спиннера
usePolling(() => {
  load(true)
  loadCounts()
}, 10_000)

// тикающая длительность running-ранов
const now = useTimeTick()

function runDuration(run: Run): string {
  return formatDuration(run.created_at, run.finished_at, run.status === 'running' ? now.value : undefined)
}

const columns: TableColumn<Run>[] = [
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'dag_name', header: 'Даг' },
  { accessorKey: 'logical_date', header: 'Дата данных' },
  { id: 'started', header: 'Запущен' },
  { id: 'duration', header: 'Длительность' },
  { id: 'actions', header: '' },
]

// клик по строке открывает ран; UTable сам игнорирует клики по кнопкам
// внутри строки
function openRun(_e: Event, row: TableRow<Run>) {
  navigateTo(`/runs/${row.original.id}`)
}

// принудительная остановка выполняющегося рана
const { canManageDag } = useAuth()
const action = useApiAction()
const cancelTarget = ref<Run | null>(null)

function canCancel(run: Run): boolean {
  return run.status === 'running' && canManageDag(run.dag_name)
}

async function confirmCancel() {
  const target = cancelTarget.value
  if (!target)
    return
  const ok = await action.run(
    () => cancelRun(target.id),
    { success: 'Ран остановлен' },
  )
  if (ok !== undefined) {
    cancelTarget.value = null
    await load()
  }
}
</script>

<template>
  <UDashboardPanel id="runs">
    <template #header>
      <UDashboardNavbar title="Раны">
        <template #right>
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить список"
            @click="load()"
          />
        </template>
      </UDashboardNavbar>
      <UDashboardToolbar>
        <template #left>
          <UTabs
            v-model="statusFilter"
            :items="statusItems"
            :content="false"
            color="neutral"
            variant="pill"
            size="sm"
          />
          <USelectMenu
            v-model="dagFilter"
            :items="dagItems"
            value-key="value"
            class="w-56"
            :search-input="{ placeholder: 'Поиск дага' }"
          />
        </template>
      </UDashboardToolbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки ранов"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <UTable
        :data="runs"
        :columns="columns"
        :loading="loading"
        :ui="{ ...denseTableUi, tr: 'cursor-pointer' }"
        @select="openRun"
      >
        <template #status-cell="{ row }">
          <StatusBadge kind="run" :status="row.original.status" size="sm" />
        </template>

        <template #dag_name-cell="{ row }">
          <!-- имя дага — только контекст строки: клик по строке ведёт в ран,
               отдельной ссылки на даг здесь нет -->
          <span class="font-medium text-highlighted">{{ row.original.dag_name }}</span>
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
          <span class="whitespace-nowrap tabular-nums">{{ runDuration(row.original) }}</span>
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end">
            <UTooltip v-if="canCancel(row.original)" text="Остановить ран">
              <UButton
                icon="i-lucide-circle-stop"
                size="sm"
                color="error"
                variant="ghost"
                aria-label="Остановить ран"
                @click="cancelTarget = row.original"
              />
            </UTooltip>
          </div>
        </template>

        <template #empty>
          <!-- при ошибке загрузки пустота — не «ранов нет», причина в алерте выше -->
          <div v-if="loadError" class="py-6" />
          <EmptyState v-else icon="i-lucide-list" title="Ранов не найдено">
            <UButton
              v-if="dagFilter !== ALL_DAGS || statusFilter !== ALL_STATUSES"
              size="sm"
              color="neutral"
              variant="subtle"
              label="Сбросить фильтры"
              @click="dagFilter = ALL_DAGS; statusFilter = ALL_STATUSES"
            />
          </EmptyState>
        </template>
      </UTable>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-end border-t border-default p-2">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" size="sm" />
      </div>

      <UModal :open="cancelTarget !== null" title="Остановить ран?" @update:open="cancelTarget = null">
        <template #body>
          <p>
            Ран <span class="font-mono font-medium">{{ cancelTarget?.id }}</span>: выполняющиеся
            таски будут убиты, а незавершённые — получат статус «остановлен». Успешные таски
            останутся успешными: ран можно будет доиграть ретраем таска.
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="cancelTarget = null" />
            <UButton color="error" label="Остановить" :loading="action.loading.value" @click="confirmCancel" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
