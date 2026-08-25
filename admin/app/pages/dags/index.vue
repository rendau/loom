<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags, listDagRegistrations, registerDag, setDagAutoUpdate, setDagPaused, syncDag } from '~/api/dag.api'
import type { Dag, DagRegistration } from '~/types/dag'

// Список дагов: узкая таблица-обзор парка (имя+бейджи, расписание, таски,
// обновлён) — образ/digest/SDK живут в карточке. Частые действия
// (запуск, пауза) — inline, остальные — в «⋯»-меню. Строка кликабельна.

const { isAdmin, canManageDag } = useAuth()

const PAGE_SIZE = 100

const dags = ref<Dag[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)
const loadError = ref('')
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
    loadError.value = ''
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

// принудительное обновление дага из registry: перерегистрация текущего
// образа в общей очереди — статус доезжает тем же поллингом регистраций
async function sync(dag: Dag) {
  const ok = await action.run(
    () => syncDag(dag.name),
    { success: `Обновление дага ${dag.name} поставлено в очередь` },
  )
  if (ok !== undefined) {
    await loadRegistrations()
    ensureRegPolling()
  }
}

// редкие действия — в «⋯»-меню строки; состав по правам (design/05:
// недоступные действия не рендерятся)
function menuItems(dag: Dag): DropdownMenuItem[][] {
  const main: DropdownMenuItem[] = []
  if (canManageDag(dag.name)) {
    main.push({ label: 'Расписание…', icon: 'i-lucide-alarm-clock', onSelect: () => { scheduleTarget.value = dag } })
    if (dag.schedule)
      main.push({ label: 'Backfill за период…', icon: 'i-lucide-calendar-clock', onSelect: () => { backfillTarget.value = dag } })
  }
  if (isAdmin.value) {
    main.push({
      label: 'Обновить из registry',
      icon: 'i-lucide-cloud-download',
      disabled: isUpdating(dag),
      onSelect: () => sync(dag),
    })
    main.push({
      label: dag.auto_update ? 'Выключить авто-обновление' : 'Включить авто-обновление',
      icon: 'i-lucide-refresh-ccw-dot',
      onSelect: () => toggleAutoUpdate(dag),
    })
  }

  const groups: DropdownMenuItem[][] = []
  if (main.length > 0)
    groups.push(main)
  if (isAdmin.value)
    groups.push([{ label: 'Удалить…', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { deleteTarget.value = dag } }])
  return groups
}

function openDag(_e: Event, row: TableRow<Dag>) {
  navigateTo(`/dags/${encodeURIComponent(row.original.name)}`)
}

const columns: TableColumn<Dag>[] = [
  { accessorKey: 'name', header: 'Даг' },
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
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить список"
            @click="load"
          />
          <UButton v-if="isAdmin" icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
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
            <UBadge v-if="row.original.paused" color="warning" variant="subtle" size="sm">
              пауза
            </UBadge>
            <UBadge v-if="isUpdating(row.original)" color="info" variant="subtle" size="sm">
              <UIcon name="i-lucide-loader-circle" class="animate-spin" />
              обновляется
            </UBadge>
            <UTooltip v-if="row.original.auto_update" text="Авто-обновление: digest тега отслеживается в registry">
              <UBadge color="info" variant="subtle" size="sm">auto</UBadge>
            </UTooltip>
          </div>
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
            <UTooltip v-if="canManageDag(row.original.name)" text="Запустить ран">
              <UButton
                icon="i-lucide-play"
                size="sm"
                color="primary"
                variant="ghost"
                aria-label="Запустить ран"
                @click="triggerTarget = row.original"
              />
            </UTooltip>
            <UTooltip v-if="canManageDag(row.original.name)" :text="row.original.paused ? 'Снять с паузы' : 'Поставить на паузу'">
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
            description="Зарегистрируйте docker-образ дага — схема и таски появятся сразу после describe."
          >
            <UButton v-if="isAdmin" size="sm" icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
          </EmptyState>
        </template>
      </UTable>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-end border-t border-default p-2">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" size="sm" />
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
