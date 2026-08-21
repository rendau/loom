<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { listRuns } from '~/api/run.api'
import type { Run } from '~/types/run'

const PAGE_SIZE = 30

const runs = ref<Run[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)

const dagFilter = ref('')
const statusFilter = ref<string | undefined>(undefined)
const statusOptions = [
  { label: 'Все статусы', value: undefined },
  { label: 'выполняется', value: 'running' },
  { label: 'успех', value: 'success' },
  { label: 'провал', value: 'failed' },
]

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
  finally {
    loading.value = false
  }
}

onMounted(load)
watch([page], load)
watch([dagFilter, statusFilter], () => {
  page.value = 1
  load()
})

const columns: TableColumn<Run>[] = [
  { accessorKey: 'id', header: 'Ран' },
  { accessorKey: 'dag_name', header: 'Даг' },
  { accessorKey: 'trigger', header: 'Триггер' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'logical_date', header: 'Лог. дата' },
  { accessorKey: 'created_at', header: 'Создан' },
  { id: 'duration', header: 'Длительность' },
]
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
      <UTable :data="runs" :columns="columns" :loading="loading">
        <template #id-cell="{ row }">
          <NuxtLink :to="`/runs/${row.original.id}`" class="font-mono text-primary hover:underline">
            {{ row.original.id }}
          </NuxtLink>
        </template>

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

        <template #logical_date-cell="{ row }">
          {{ formatDateTime(row.original.logical_date) }}
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #duration-cell="{ row }">
          {{ formatDuration(row.original.created_at, row.original.finished_at) }}
        </template>
      </UTable>

      <div v-if="!loading && runs.length === 0" class="p-8 text-center text-muted">
        Ранов не найдено.
      </div>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-center border-t border-default p-3">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" />
      </div>
    </template>
  </UDashboardPanel>
</template>
