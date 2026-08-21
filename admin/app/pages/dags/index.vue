<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { listDags, registerDag, setDagAutoUpdate, setDagPaused, deleteDag } from '~/api/dag.api'
import { backfillRuns, triggerRun } from '~/api/run.api'
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
const registerAutoUpdate = ref(false)

async function submitRegister() {
  const image = registerImage.value.trim()
  if (!image)
    return
  const rep = await action.run(() => registerDag(image, registerAutoUpdate.value), { success: 'Даг зарегистрирован' })
  if (rep) {
    registerOpen.value = false
    registerImage.value = ''
    registerAutoUpdate.value = false
    await load()
  }
}

// ручной триггер: опциональные params (JSON-объект)
const triggerTarget = ref<Dag | null>(null)
const triggerParams = ref('')

// parseParams валидирует JSON-объект параметров; null — ошибка ввода.
function parseParams(raw: string): Record<string, unknown> | undefined | null {
  const trimmed = raw.trim()
  if (!trimmed)
    return undefined
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      return null
    return parsed as Record<string, unknown>
  }
  catch {
    return null
  }
}

const toast = useToast()

function badParamsToast() {
  toast.add({ title: 'Параметры должны быть JSON-объектом', color: 'error' })
}

async function confirmTrigger() {
  const dag = triggerTarget.value
  if (!dag)
    return
  const params = parseParams(triggerParams.value)
  if (params === null) {
    badParamsToast()
    return
  }
  const rep = await action.run(() => triggerRun(dag.name, params), { success: 'Ран запущен' })
  if (rep) {
    triggerTarget.value = null
    triggerParams.value = ''
    await navigateTo(`/runs/${rep.run_id}`)
  }
}

// backfill: ран на каждый тик расписания в периоде
const backfillTarget = ref<Dag | null>(null)
const backfillFrom = ref('')
const backfillTo = ref('')
const backfillParams = ref('')

async function confirmBackfill() {
  const dag = backfillTarget.value
  if (!dag)
    return
  if (!backfillFrom.value || !backfillTo.value) {
    toast.add({ title: 'Задайте период from и to', color: 'error' })
    return
  }
  const params = parseParams(backfillParams.value)
  if (params === null) {
    badParamsToast()
    return
  }

  const from = new Date(backfillFrom.value).toISOString()
  const to = new Date(backfillTo.value).toISOString()
  const rep = await action.run(() => backfillRuns(dag.name, from, to, params))
  if (rep) {
    toast.add({ title: `Создано ранов: ${rep.run_ids.length}`, color: 'success' })
    backfillTarget.value = null
    backfillFrom.value = ''
    backfillTo.value = ''
    backfillParams.value = ''
    await navigateTo('/runs')
  }
}

async function togglePaused(dag: Dag) {
  const ok = await action.run(
    () => setDagPaused(dag.name, !dag.paused),
    { success: dag.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
  )
  if (ok !== undefined)
    await load()
}

async function toggleAutoUpdate(dag: Dag) {
  const ok = await action.run(
    () => setDagAutoUpdate(dag.name, !dag.auto_update),
    { success: dag.auto_update ? 'Авто-обновление выключено' : 'Авто-обновление включено' },
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
            <div class="flex items-center gap-1.5">
              <span class="font-mono">{{ row.original.schedule }}</span>
              <UBadge v-if="row.original.catchup" color="info" variant="subtle" size="sm">catchup</UBadge>
            </div>
            <div class="text-xs text-muted">след.: {{ formatDateTime(row.original.next_run_at) }}</div>
          </div>
          <span v-else class="text-muted">—</span>
        </template>

        <template #tasks-cell="{ row }">
          {{ row.original.tasks.length }}
        </template>

        <template #image-cell="{ row }">
          <div class="flex items-center gap-1.5">
            <span class="font-mono text-xs">{{ row.original.image }}</span>
            <UTooltip v-if="row.original.auto_update" text="Авто-обновление: digest тега отслеживается в registry">
              <UBadge color="info" variant="subtle" size="sm">auto</UBadge>
            </UTooltip>
          </div>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip text="Запустить ран">
              <UButton icon="i-lucide-play" size="sm" color="primary" variant="ghost" @click="triggerTarget = row.original" />
            </UTooltip>
            <UTooltip v-if="row.original.schedule" text="Backfill за период">
              <UButton icon="i-lucide-calendar-clock" size="sm" color="secondary" variant="ghost" @click="backfillTarget = row.original" />
            </UTooltip>
            <UTooltip :text="row.original.auto_update ? 'Выключить авто-обновление образа' : 'Включить авто-обновление образа'">
              <UButton
                icon="i-lucide-refresh-ccw-dot"
                size="sm"
                :color="row.original.auto_update ? 'info' : 'neutral'"
                variant="ghost"
                @click="toggleAutoUpdate(row.original)"
              />
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
          <div class="space-y-4">
            <UFormField label="Docker-образ" hint="например registry/my-dag:latest">
              <UInput
                v-model="registerImage"
                class="w-full"
                placeholder="registry/my-dag:latest"
                autofocus
                @keyup.enter="submitRegister"
              />
            </UFormField>
            <UCheckbox
              v-model="registerAutoUpdate"
              label="Авто-обновление образа"
              description="Server будет следить за digest'ом тега в registry и перерегистрировать даг при изменении."
            />
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="registerOpen = false" />
            <UButton label="Зарегистрировать" :loading="action.loading.value" @click="submitRegister" />
          </div>
        </template>
      </UModal>

      <!-- ручной запуск рана (с опциональными параметрами) -->
      <UModal
        :open="triggerTarget !== null"
        title="Запуск рана"
        :description="`Даг ${triggerTarget?.name ?? ''}. Параметры доступны таскам через rt.Params().`"
        @update:open="triggerTarget = null"
      >
        <template #body>
          <UFormField label="Параметры (JSON-объект, опционально)">
            <UTextarea
              v-model="triggerParams"
              class="w-full font-mono"
              :rows="4"
              placeholder='{"date": "2026-08-01"}'
            />
          </UFormField>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="triggerTarget = null" />
            <UButton label="Запустить" :loading="action.loading.value" @click="confirmTrigger" />
          </div>
        </template>
      </UModal>

      <!-- backfill за период -->
      <UModal
        :open="backfillTarget !== null"
        title="Backfill"
        :description="`Ран на каждый тик расписания «${backfillTarget?.schedule ?? ''}» в периоде [from, to).`"
        @update:open="backfillTarget = null"
      >
        <template #body>
          <div class="space-y-4">
            <div class="grid grid-cols-2 gap-3">
              <UFormField label="From (включительно)">
                <UInput v-model="backfillFrom" type="datetime-local" class="w-full" />
              </UFormField>
              <UFormField label="To (исключительно)">
                <UInput v-model="backfillTo" type="datetime-local" class="w-full" />
              </UFormField>
            </div>
            <UFormField label="Параметры всех ранов (JSON-объект, опционально)">
              <UTextarea
                v-model="backfillParams"
                class="w-full font-mono"
                :rows="3"
                placeholder='{"source": "backfill"}'
              />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="backfillTarget = null" />
            <UButton label="Создать раны" :loading="action.loading.value" @click="confirmBackfill" />
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
