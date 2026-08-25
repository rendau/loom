<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags, listDagRegistrations, registerDag, setDagAutoUpdate, setDagPaused, syncDag } from '~/api/dag.api'
import { listPools } from '~/api/pool.api'
import type { Dag, DagRegistration } from '~/types/dag'
import type { Pool } from '~/types/pool'

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

// даг требует переменных, значения которых ещё не заведены: до первого
// запуска это больше нигде не видно
const envGaps = useDagEnvGaps()

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
    // пачка доезжает разом — тосты по одному завалили бы экран, поэтому
    // за тик поллинга не больше двух: успехи и провалы одной сводкой
    const done = finished.map(id => registrations.value.find(r => r.id === id)).filter(r => !!r)
    const succeeded = done.filter(r => r.status === 'success')
    const errored = done.filter(r => r.status === 'failed')
    if (succeeded.length === 1)
      toast.add({ title: `Даг ${succeeded[0]!.dag_name} зарегистрирован`, color: 'success' })
    else if (succeeded.length > 1) {
      toast.add({
        title: `Дагов зарегистрировано: ${succeeded.length}`,
        description: succeeded.map(r => r.dag_name).join(', '),
        color: 'success',
      })
    }
    if (errored.length === 1)
      toast.add({ title: `Регистрация ${errored[0]!.image} не удалась`, description: errored[0]!.error, color: 'error' })
    else if (errored.length > 1) {
      toast.add({
        title: `Регистраций не удалось: ${errored.length}`,
        description: 'Причины — в панели над списком дагов.',
        color: 'error',
      })
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
  await Promise.all([load(), loadPools()])
  await loadRegistrations()
  if (activeRegistrations.value.length > 0)
    ensureRegPolling()
})

onUnmounted(stopRegPolling)

// регистрация дагов по образам (асинхронная: describe выполняется в фоне);
// образов можно ввести сразу несколько — по строке, через запятую и т.п.
const registerOpen = ref(false)
const registerImages = ref('')
const registerBusy = ref(false)
const registerAutoUpdate = ref(false)
const registerSchedule = ref('')
const registerCatchup = ref(false)
const registerStartScheduled = ref(true)

// пул дага: действует на все его таски (в коде дага пула нет).
// reka-ui Select не принимает '' как value — «не задан» через sentinel.
const NO_POOL = '__none__'
const registerPool = ref(NO_POOL)
const pools = ref<Pool[]>([])

const poolItems = computed(() => [
  { label: 'default (общий пул)', value: NO_POOL },
  ...pools.value.map(p => ({ label: `${p.name} · ${p.slots} слотов`, value: p.name })),
])

const registerImageList = computed(() => parseImageList(registerImages.value))

async function loadPools() {
  try {
    pools.value = (await listPools()).results ?? []
  }
  catch {
    // селект останется с одним «не задан» — регистрация всё равно возможна
  }
}

async function submitRegister() {
  const images = registerImageList.value
  if (images.length === 0)
    return

  const schedule = registerSchedule.value.trim()
  const settings = {
    auto_update: registerAutoUpdate.value,
    schedule: schedule || undefined,
    catchup: schedule ? registerCatchup.value : undefined,
    paused: schedule ? !registerStartScheduled.value : undefined,
    pool: registerPool.value === NO_POOL ? undefined : registerPool.value,
  }

  // RegisterDag принимает один образ — ставим в очередь по одному, но
  // частичный успех не теряем: упавшие остаются в поле для повтора
  const failed: { image: string, error: string }[] = []
  registerBusy.value = true
  try {
    for (const image of images) {
      try {
        await registerDag({ image, ...settings })
      }
      catch (error) {
        failed.push({ image, error: apiErrorMessage(error) })
      }
    }
  }
  finally {
    registerBusy.value = false
  }

  const queued = images.length - failed.length
  if (queued > 0) {
    toast.add({
      title: queued === 1 ? 'Регистрация поставлена в очередь' : `Регистраций поставлено в очередь: ${queued}`,
      color: 'success',
    })
    await loadRegistrations()
    ensureRegPolling()
  }
  for (const f of failed)
    toast.add({ title: `Не удалось поставить в очередь ${f.image}`, description: f.error, color: 'error' })

  if (failed.length > 0) {
    registerImages.value = failed.map(f => f.image).join('\n')
    return
  }

  registerOpen.value = false
  registerImages.value = ''
  registerAutoUpdate.value = false
  registerSchedule.value = ''
  registerCatchup.value = false
  registerStartScheduled.value = true
  registerPool.value = NO_POOL
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

      <!-- активные и недавно провалившиеся регистрации: пачка сворачивается
           в одну строку, иначе список дагов уезжает за экран -->
      <DagRegistrationQueue :active="activeRegistrations" :failed="failedRegistrations" />

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
            <UTooltip v-if="envGaps.missing(row.original.name)" text="Не заполнены переменные или секреты — запуск таска упадёт launch_failed">
              <UBadge color="error" variant="subtle" size="sm">
                <UIcon name="i-lucide-triangle-alert" class="size-3" />
                env: {{ envGaps.missing(row.original.name) }}
              </UBadge>
            </UTooltip>
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
      <UModal v-model:open="registerOpen" title="Регистрация дагов" description="Pull образов и describe выполняются в фоне — статус появится в панели над таблицей.">
        <template #body>
          <div class="space-y-4">
            <UFormField label="Docker-образы" hint="можно списком: перенос строки, запятая, ; или пробел">
              <UTextarea
                v-model="registerImages"
                class="w-full font-mono"
                :rows="3"
                autoresize
                :maxrows="12"
                placeholder="registry/my-dag:latest&#10;registry/other-dag:v2"
                autofocus
              />
            </UFormField>
            <!-- разбор списка виден до отправки: настройки ниже уедут в каждый образ -->
            <div v-if="registerImageList.length > 1" class="space-y-1.5">
              <p class="text-xs text-muted">
                Образов: {{ registerImageList.length }} — настройки ниже применятся к каждому.
              </p>
              <div class="flex flex-wrap gap-1">
                <UBadge
                  v-for="image in registerImageList"
                  :key="image"
                  color="neutral"
                  variant="subtle"
                  size="sm"
                  class="font-mono"
                >
                  {{ image }}
                </UBadge>
              </div>
            </div>
            <UCheckbox
              v-model="registerAutoUpdate"
              label="Авто-обновление образа"
              description="Server будет следить за digest'ом тега в registry и перерегистрировать даг при изменении."
            />
            <UFormField label="Пул слотов" description="Действует на все таски дага.">
              <USelectMenu v-model="registerPool" :items="poolItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField label="Cron-расписание (опционально)" hint="пусто — запуск вручную">
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
            <UButton
              :label="registerImageList.length > 1 ? `Зарегистрировать (${registerImageList.length})` : 'Зарегистрировать'"
              :disabled="registerImageList.length === 0"
              :loading="registerBusy"
              @click="submitRegister"
            />
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
