<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { getDashboard } from '~/api/dashboard.api'
import { listPools, setPool } from '~/api/pool.api'
import type { Pool } from '~/types/pool'

// Пулы слотов параллелизма: таски всех дагов конкурируют за слоты своего
// пула. Удаления нет — на пул могут ссылаться манифесты; slots = 0 ставит
// пул на паузу. Занятость (busy) отдаёт только dashboard RPC — тянем его
// же (отдельного метода в ListPool нет, design/07 №5-6).

const { isAdmin } = useAuth()

const pools = ref<Pool[]>([])
const busyByName = ref<Map<string, number>>(new Map())
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    const [rep, dashboard] = await Promise.all([
      listPools(),
      getDashboard().catch(() => null), // занятость — best effort
    ])
    pools.value = rep.results ?? []
    busyByName.value = new Map(
      (dashboard?.pools ?? []).map(p => [p.name, Number(p.busy)]))
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

onMounted(load)
usePolling(() => load(true), 10_000)

// создание пула / изменение слотов
const editOpen = ref(false)
const editName = ref('')
const editSlots = ref(1)
const editIsNew = ref(true)

function openCreate() {
  editIsNew.value = true
  editName.value = ''
  editSlots.value = 1
  editOpen.value = true
}

function openEdit(pool: Pool) {
  editIsNew.value = false
  editName.value = pool.name
  editSlots.value = pool.slots
  editOpen.value = true
}

async function submitEdit() {
  const name = editName.value.trim()
  if (!name)
    return
  const ok = await action.run(() => setPool(name, editSlots.value), { success: 'Пул сохранён' })
  if (ok !== undefined) {
    editOpen.value = false
    await load()
  }
}

const columns: TableColumn<Pool>[] = [
  { accessorKey: 'name', header: 'Пул' },
  { accessorKey: 'slots', header: 'Слоты' },
  { id: 'busy', header: 'Занято' },
  { accessorKey: 'created_at', header: 'Создан' },
  { accessorKey: 'modified_at', header: 'Изменён' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="pools">
    <template #header>
      <UDashboardNavbar title="Пулы">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" aria-label="Обновить список" @click="load()" />
          <UButton v-if="isAdmin" icon="i-lucide-plus" label="Создать" @click="openCreate" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки пулов"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <UTable :data="pools" :columns="columns" :loading="loading" :ui="denseTableUi">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-highlighted">{{ row.original.name }}</span>
            <UBadge v-if="row.original.slots === 0" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
          </div>
        </template>

        <template #busy-cell="{ row }">
          <div v-if="row.original.slots > 0" class="flex w-36 items-center gap-2">
            <span class="shrink-0 text-xs tabular-nums text-muted">
              {{ busyByName.get(row.original.name) ?? 0 }} / {{ row.original.slots }}
            </span>
            <div class="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-elevated">
              <div
                class="h-full rounded-full"
                :class="(busyByName.get(row.original.name) ?? 0) >= row.original.slots ? 'bg-warning' : 'bg-primary'"
                :style="{ width: `${Math.min(100, ((busyByName.get(row.original.name) ?? 0) / row.original.slots) * 100)}%` }"
              />
            </div>
          </div>
          <span v-else class="text-muted">—</span>
        </template>

        <template #created_at-cell="{ row }">
          <RelativeTime :time="row.original.created_at" />
        </template>

        <template #modified_at-cell="{ row }">
          <RelativeTime :time="row.original.modified_at" />
        </template>

        <template #actions-cell="{ row }">
          <div v-if="isAdmin" class="flex justify-end">
            <UButton
              icon="i-lucide-pencil"
              size="sm"
              color="neutral"
              variant="ghost"
              label="Слоты"
              @click="openEdit(row.original)"
            />
          </div>
        </template>
      </UTable>

      <div v-if="!loading && !loadError && pools.length === 0">
        <EmptyState
          icon="i-lucide-layers"
          title="Пулов нет"
          description="Таск попадает в пул опцией loom.Pool(name); слоты ограничивают число одновременных попыток."
        >
          <UButton v-if="isAdmin" size="sm" icon="i-lucide-plus" label="Создать" @click="openCreate" />
        </EmptyState>
      </div>

      <!-- создание / изменение слотов -->
      <UModal
        v-model:open="editOpen"
        :title="editIsNew ? 'Создание пула' : `Пул ${editName}`"
        description="Таск попадает в пул опцией loom.Pool(name); слоты ограничивают число одновременных попыток. 0 — пауза пула."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="editIsNew" label="Имя пула">
              <UInput v-model="editName" class="w-full" placeholder="db-heavy" autofocus />
            </UFormField>
            <UFormField label="Слоты">
              <UInputNumber v-model="editSlots" :min="0" :max="10000" class="w-full" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
            <UButton label="Сохранить" :loading="action.loading.value" @click="submitEdit" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
