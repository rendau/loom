<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { listDags, registerDag, setDagPaused, deleteDag } from '~/api/dag.api'
import { triggerRun } from '~/api/run.api'
import type { Dag } from '~/types/dag'

const dags = ref<Dag[]>([])
const loading = ref(false)
const action = useApiAction()

async function load() {
  loading.value = true
  try {
    const rep = await listDags({ list_params: { page_size: 500, sort: ['name'] } })
    dags.value = rep.results
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

// регистрация дага по образу
const registerOpen = ref(false)
const registerImage = ref('')

async function submitRegister() {
  const image = registerImage.value.trim()
  if (!image)
    return
  const rep = await action.run(() => registerDag(image), { success: 'Даг зарегистрирован' })
  if (rep) {
    registerOpen.value = false
    registerImage.value = ''
    await load()
  }
}

// действия по строке
async function trigger(dag: Dag) {
  const rep = await action.run(() => triggerRun(dag.name), { success: 'Ран запущен' })
  if (rep)
    await navigateTo(`/runs/${rep.run_id}`)
}

async function togglePaused(dag: Dag) {
  const ok = await action.run(
    () => setDagPaused(dag.name, !dag.paused),
    { success: dag.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
  )
  if (ok !== undefined)
    await load()
}

const deleteTarget = ref<Dag | null>(null)

async function confirmDelete() {
  const dag = deleteTarget.value
  if (!dag)
    return
  const ok = await action.run(() => deleteDag(dag.name), { success: 'Даг удалён' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<Dag>[] = [
  { accessorKey: 'name', header: 'Даг' },
  { accessorKey: 'schedule', header: 'Расписание' },
  { id: 'tasks', header: 'Тасков' },
  { accessorKey: 'image', header: 'Образ' },
  { accessorKey: 'sdk_version', header: 'SDK' },
  { accessorKey: 'created_at', header: 'Создан' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="dags">
    <template #header>
      <UDashboardNavbar title="Даги">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
          <UButton icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UTable :data="dags" :columns="columns" :loading="loading">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-highlighted">{{ row.original.name }}</span>
            <UBadge v-if="row.original.paused" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
          </div>
        </template>

        <template #schedule-cell="{ row }">
          <div v-if="row.original.schedule">
            <div class="font-mono">{{ row.original.schedule }}</div>
            <div class="text-xs text-muted">след.: {{ formatDateTime(row.original.next_run_at) }}</div>
          </div>
          <span v-else class="text-muted">—</span>
        </template>

        <template #tasks-cell="{ row }">
          {{ row.original.tasks.length }}
        </template>

        <template #image-cell="{ row }">
          <span class="font-mono text-xs">{{ row.original.image }}</span>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip text="Запустить ран">
              <UButton icon="i-lucide-play" size="sm" color="primary" variant="ghost" @click="trigger(row.original)" />
            </UTooltip>
            <UTooltip :text="row.original.paused ? 'Снять с паузы' : 'Поставить на паузу'">
              <UButton
                :icon="row.original.paused ? 'i-lucide-play-circle' : 'i-lucide-pause-circle'"
                size="sm"
                color="warning"
                variant="ghost"
                @click="togglePaused(row.original)"
              />
            </UTooltip>
            <UTooltip text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <div v-if="!loading && dags.length === 0" class="p-8 text-center text-muted">
        Дагов пока нет — зарегистрируйте docker-образ дага.
      </div>

      <!-- регистрация дага -->
      <UModal v-model:open="registerOpen" title="Регистрация дага" description="Server сделает pull образа, запустит describe и сохранит манифест.">
        <template #body>
          <UFormField label="Docker-образ" hint="например registry/my-dag:latest">
            <UInput
              v-model="registerImage"
              class="w-full"
              placeholder="registry/my-dag:latest"
              autofocus
              @keyup.enter="submitRegister"
            />
          </UFormField>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="registerOpen = false" />
            <UButton label="Зарегистрировать" :loading="action.loading.value" @click="submitRegister" />
          </div>
        </template>
      </UModal>

      <!-- подтверждение удаления -->
      <UModal :open="deleteTarget !== null" title="Удалить даг?" @update:open="deleteTarget = null">
        <template #body>
          <p>
            Даг <span class="font-mono font-medium">{{ deleteTarget?.name }}</span> будет удалён.
            История его ранов останется.
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="deleteTarget = null" />
            <UButton color="error" label="Удалить" :loading="action.loading.value" @click="confirmDelete" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
