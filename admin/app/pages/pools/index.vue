<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { listPools, setPool } from '~/api/pool.api'
import type { Pool } from '~/types/pool'

// Пулы слотов параллелизма: таски всех дагов конкурируют за слоты своего
// пула. Удаления нет — на пул могут ссылаться манифесты; slots = 0 ставит
// пул на паузу.

const pools = ref<Pool[]>([])
const loading = ref(false)
const action = useApiAction()

async function load() {
  loading.value = true
  try {
    const rep = await listPools()
    pools.value = rep.results ?? []
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

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
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
          <UButton icon="i-lucide-plus" label="Создать" @click="openCreate" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UTable :data="pools" :columns="columns" :loading="loading">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-highlighted">{{ row.original.name }}</span>
            <UBadge v-if="row.original.slots === 0" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
          </div>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #modified_at-cell="{ row }">
          {{ formatDateTime(row.original.modified_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end">
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

      <div v-if="!loading && pools.length === 0" class="p-8 text-center text-muted">
        Пулов нет.
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
