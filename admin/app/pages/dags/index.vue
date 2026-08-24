<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags, listDagRegistrations, registerDag, setDagAutoUpdate, setDagPaused } from '~/api/dag.api'
import type { Dag, DagRegistration } from '~/types/dag'

const { isAdmin, canManageDag } = useAuth()

const PAGE_SIZE = 30

const dags = ref<Dag[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)
const action = useApiAction()

async function load() {
  loading.value = true
  try {
    const rep = await listDags({
      list_params: {
        page: page.value - 1,
        page_size: PAGE_SIZE,
        with_total_count: true,
        sort: ['name'],
      },
    })
    dags.value = rep.results
    totalCount.value = Number(rep.pagination_info.total_count)
  }
  catch (error) {
    toast.add({ title: 'Ошибка загрузки дагов', description: apiErrorMessage(error), color: 'error' })
  }
  finally {
    loading.value = false
  }
}

watch(page, load)

// ── очередь регистраций: панель статусов + поллинг, пока есть активные ──

const registrations = ref<DagRegistration[]>([])
let regTimer: ReturnType<typeof setInterval> | undefined

const activeRegistrations = computed(() =>
  registrations.value.filter(r => r.status === 'pending' || r.status === 'running'))

// провалы за последние сутки: у новой (несозданной) записи дага это
// единственное место увидеть причину
const failedRegistrations = computed(() => {
  const dayAgo = Date.now() - 24 * 60 * 60 * 1000
  return registrations.value.filter(r =>
    r.status === 'failed' && r.finished_at && new Date(r.finished_at).getTime() > dayAgo)
})

// даг «обновляется»: активная регистрация с его именем (auto) или образом
function isUpdating(dag: Dag): boolean {
  return activeRegistrations.value.some(r => r.dag_name === dag.name || r.image === dag.image)
}

async function loadRegistrations() {
  registrations.value = (await listDagRegistrations({ limit: 50 })).results
}

function ensureRegPolling() {
  if (regTimer)
    return
  regTimer = setInterval(async () => {
    const wasActive = new Set(activeRegistrations.value.map(r => r.id))
    await loadRegistrations()

    const nowActive = new Set(activeRegistrations.value.map(r => r.id))
    const finished = [...wasActive].filter(id => !nowActive.has(id))
    if (finished.length > 0)
      await load()
    for (const id of finished) {
      const reg = registrations.value.find(r => r.id === id)
      if (!reg)
        continue
      if (reg.status === 'success')
        toast.add({ title: `Даг ${reg.dag_name} зарегистрирован`, color: 'success' })
      else if (reg.status === 'failed')
        toast.add({ title: `Регистрация ${reg.image} не удалась`, description: reg.error, color: 'error' })
    }

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

onMounted(async () => {
  await load()
  await loadRegistrations()
  if (activeRegistrations.value.length > 0)
    ensureRegPolling()
})

onUnmounted(stopRegPolling)

// регистрация дага по образу (асинхронная: describe выполняется в фоне)
const registerOpen = ref(false)
const registerImage = ref('')
const registerAutoUpdate = ref(false)
const registerSchedule = ref('')
const registerCatchup = ref(false)
const registerStartScheduled = ref(true)

async function submitRegister() {
  const image = registerImage.value.trim()
  if (!image)
    return

  const schedule = registerSchedule.value.trim()
  const rep = await action.run(() => registerDag({
    image,
    auto_update: registerAutoUpdate.value,
    schedule: schedule || undefined,
    catchup: schedule ? registerCatchup.value : undefined,
    paused: schedule ? !registerStartScheduled.value : undefined,
  }), { success: 'Регистрация поставлена в очередь' })

  if (rep) {
    registerOpen.value = false
    registerImage.value = ''
    registerAutoUpdate.value = false
    registerSchedule.value = ''
    registerCatchup.value = false
    registerStartScheduled.value = true
    await loadRegistrations()
    ensureRegPolling()
  }
}

// модалки над дагом — общие компоненты (используются и карточкой дага)
const triggerTarget = ref<Dag | null>(null)
const backfillTarget = ref<Dag | null>(null)
const deleteTarget = ref<Dag | null>(null)

const toast = useToast()

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
          <UButton v-if="isAdmin" icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <!-- активные и недавно провалившиеся регистрации -->
      <div v-if="activeRegistrations.length > 0 || failedRegistrations.length > 0" class="space-y-2">
        <UAlert
          v-for="reg in activeRegistrations"
          :key="reg.id"
          color="info"
          variant="subtle"
          icon="i-lucide-loader-circle"
          :title="`Регистрация ${reg.image}`"
          :description="reg.status === 'pending' ? 'В очереди…' : 'Выполняется pull + describe…'"
        />
        <UAlert
          v-for="reg in failedRegistrations"
          :key="reg.id"
          color="error"
          variant="subtle"
          icon="i-lucide-circle-x"
          :title="`Регистрация ${reg.dag_name || reg.image} не удалась (${formatDateTime(reg.finished_at)})`"
          :description="reg.error"
        />
      </div>

      <UTable :data="dags" :columns="columns" :loading="loading">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <NuxtLink :to="`/dags/${encodeURIComponent(row.original.name)}`" class="font-medium text-highlighted hover:text-primary hover:underline">
              {{ row.original.name }}
            </NuxtLink>
            <UBadge v-if="row.original.paused" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
            <UBadge v-if="isUpdating(row.original)" color="info" variant="subtle" size="sm">
              <UIcon name="i-lucide-loader-circle" class="animate-spin" />
              обновляется
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
            <UTooltip v-if="canManageDag(row.original.name)" text="Запустить ран">
              <UButton icon="i-lucide-play" size="sm" color="primary" variant="ghost" @click="triggerTarget = row.original" />
            </UTooltip>
            <UTooltip v-if="row.original.schedule && canManageDag(row.original.name)" text="Backfill за период">
              <UButton icon="i-lucide-calendar-clock" size="sm" color="secondary" variant="ghost" @click="backfillTarget = row.original" />
            </UTooltip>
            <UTooltip v-if="canManageDag(row.original.name)" text="Расписание">
              <UButton icon="i-lucide-alarm-clock" size="sm" color="neutral" variant="ghost" @click="scheduleTarget = row.original" />
            </UTooltip>
            <UTooltip v-if="isAdmin" :text="row.original.auto_update ? 'Выключить авто-обновление образа' : 'Включить авто-обновление образа'">
              <UButton
                icon="i-lucide-refresh-ccw-dot"
                size="sm"
                :color="row.original.auto_update ? 'info' : 'neutral'"
                variant="ghost"
                @click="toggleAutoUpdate(row.original)"
              />
            </UTooltip>
            <UTooltip v-if="canManageDag(row.original.name)" :text="row.original.paused ? 'Снять с паузы' : 'Поставить на паузу'">
              <UButton
                :icon="row.original.paused ? 'i-lucide-play-circle' : 'i-lucide-pause-circle'"
                size="sm"
                color="warning"
                variant="ghost"
                @click="togglePaused(row.original)"
              />
            </UTooltip>
            <UTooltip v-if="isAdmin" text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <div v-if="!loading && dags.length === 0" class="p-8 text-center text-muted">
        Дагов пока нет — зарегистрируйте docker-образ дага.
      </div>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-center border-t border-default p-3">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" />
      </div>

      <!-- регистрация дага -->
      <UModal v-model:open="registerOpen" title="Регистрация дага" description="Pull образа и describe выполняются в фоне — статус появится в панели над таблицей.">
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
            <UFormField label="Cron-расписание (опционально)" hint="только для нового дага; пусто — запуск вручную">
              <UInput
                v-model="registerSchedule"
                class="w-full font-mono"
                placeholder="0 3 * * *"
              />
            </UFormField>
            <template v-if="registerSchedule.trim()">
              <UCheckbox
                v-model="registerStartScheduled"
                label="Включить расписание сразу"
                description="Выключено — даг создаётся на паузе: расписание сохранится, но запуски только вручную до снятия паузы."
              />
              <UCheckbox
                v-model="registerCatchup"
                label="Catchup"
                description="Наверстывать пропущенные тики расписания (ран на каждый тик, logical_date = тик)."
              />
            </template>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="registerOpen = false" />
            <UButton label="Зарегистрировать" :loading="action.loading.value" @click="submitRegister" />
          </div>
        </template>
      </UModal>

      <DagTriggerModal :dag="triggerTarget" @close="triggerTarget = null" />
      <DagBackfillModal :dag="backfillTarget" @close="backfillTarget = null" />
      <DagScheduleModal :dag="scheduleTarget" @close="scheduleTarget = null" @saved="onScheduleSaved" />
      <DagDeleteModal :dag="deleteTarget" @close="deleteTarget = null" @deleted="onDeleted" />
    </template>
  </UDashboardPanel>
</template>
