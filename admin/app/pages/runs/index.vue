<script setup lang="ts">
import type { TableColumn, TableRow } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { cancelRun, listRuns } from '~/api/run.api'
import type { Run } from '~/types/run'

const PAGE_SIZE = 30

const runs = ref<Run[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)

// фильтр по дагу инициализируется из query (?dag_name=...) — так на раны
// конкретного дага ведут ссылки с его карточки
const route = useRoute()
const dagFilter = ref(String(route.query.dag_name ?? ''))
const statusFilter = ref<string | undefined>(
  typeof route.query.status === 'string' ? route.query.status : undefined,
)
const statusOptions = [
  { label: 'Все статусы', value: undefined },
  { label: 'выполняется', value: 'running' },
  { label: 'успех', value: 'success' },
  { label: 'провал', value: 'failed' },
  { label: 'остановлен', value: 'canceled' },
]

const toast = useToast()

async function load() {
  loading.value = true
  try {
    const rep = await listRuns({
      list_params: {
        page: page.value - 1,
        page_size: PAGE_SIZE,
        with_total_count: true,
        sort: ['-created_at'],
      },
      dag_name: dagFilter.value.trim() || undefined,
      status: statusFilter.value,
    })
    runs.value = rep.results
    totalCount.value = Number(rep.pagination_info.total_count)
  }
  catch (error) {
    toast.add({ title: 'Ошибка загрузки ранов', description: apiErrorMessage(error), color: 'error' })
  }
  finally {
    loading.value = false
  }
}

onMounted(load)
watch([page], load)

// текстовый фильтр — с debounce (иначе запрос на каждый символ)
const reloadFiltered = debounceFn(() => {
  page.value = 1
  load()
})
watch(dagFilter, reloadFiltered)
watch(statusFilter, () => {
  page.value = 1
  load()
})

// id рана и логическая дата в таблицу не выведены (она и так широкая): id
// виден в карточке рана, куда ведёт клик по строке, там же — логическая дата
const columns: TableColumn<Run>[] = [
  { accessorKey: 'dag_name', header: 'Даг' },
  { accessorKey: 'trigger', header: 'Триггер' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'created_at', header: 'Создан' },
  { id: 'duration', header: 'Длительность' },
  { id: 'actions', header: '' },
]

// клик по строке открывает ран; UTable сам игнорирует клики по кнопкам и
// ссылкам внутри строки, так что иконка остановки продолжает работать
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
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
        </template>
      </UDashboardNavbar>
      <UDashboardToolbar>
        <template #left>
          <UInput
            v-model="dagFilter"
            icon="i-lucide-search"
            placeholder="Фильтр по дагу"
            class="w-56"
          />
          <USelect
            v-model="statusFilter"
            :items="statusOptions"
            value-key="value"
            class="w-44"
          />
        </template>
      </UDashboardToolbar>
    </template>

    <template #body>
      <UTable
        :data="runs"
        :columns="columns"
        :loading="loading"
        :ui="{ tr: 'cursor-pointer' }"
        @select="openRun"
      >
        <template #trigger-cell="{ row }">
          <div class="flex items-center gap-1.5">
            <UBadge :color="runTriggerColor(row.original.trigger)" variant="subtle" size="sm">
              {{ runTriggerLabel(row.original.trigger) }}
            </UBadge>
            <UTooltip v-if="row.original.params" text="Ран с параметрами">
              <UIcon name="i-lucide-braces" class="size-3.5 text-muted" />
            </UTooltip>
          </div>
        </template>

        <template #status-cell="{ row }">
          <UBadge :color="runStatusColor(row.original.status)" variant="subtle">
            {{ runStatusLabel(row.original.status) }}
          </UBadge>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #duration-cell="{ row }">
          {{ formatDuration(row.original.created_at, row.original.finished_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end">
            <UTooltip v-if="canCancel(row.original)" text="Остановить ран">
              <UButton
                icon="i-lucide-circle-stop"
                size="sm"
                color="error"
                variant="ghost"
                @click="cancelTarget = row.original"
              />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <div v-if="!loading && runs.length === 0" class="p-8 text-center text-muted">
        Ранов не найдено.
      </div>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-center border-t border-default p-3">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" />
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
