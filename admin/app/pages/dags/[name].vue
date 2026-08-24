<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { getDag, listDagRegistrations, setDagAutoUpdate, setDagPaused } from '~/api/dag.api'
import type { Dag, DagRegistration, DagTask } from '~/types/dag'

// Карточка дага: схема (граф) и параметры тасков видны сразу после
// регистрации, до первого запуска; здесь же история регистраций.

const route = useRoute()
const dagName = String(route.params.name)

const { isAdmin, canManageDag } = useAuth()
const canManage = computed(() => canManageDag(dagName))

const dag = ref<Dag | null>(null)
const registrations = ref<DagRegistration[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

const isUpdating = computed(() =>
  registrations.value.some(r => r.status === 'pending' || r.status === 'running'))

async function load(background = false) {
  if (!background)
    loading.value = true
  try {
    dag.value = await getDag(dagName)
    registrations.value = (await listDagRegistrations({ dag_name: dagName, limit: 20 })).results
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

// пока идёт регистрация/обновление — фоновый поллинг
let pollTimer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  await load()
  pollTimer = setInterval(() => {
    if (isUpdating.value)
      load(true)
  }, 3000)
})
onBeforeUnmount(() => clearInterval(pollTimer))

// действия
const triggerTarget = ref<Dag | null>(null)
const backfillTarget = ref<Dag | null>(null)
const scheduleTarget = ref<Dag | null>(null)
const deleteTarget = ref<Dag | null>(null)

async function onScheduleSaved() {
  scheduleTarget.value = null
  await load()
}

async function onDeleted() {
  deleteTarget.value = null
  await navigateTo('/dags')
}

async function togglePaused() {
  const d = dag.value
  if (!d)
    return
  const ok = await action.run(
    () => setDagPaused(d.name, !d.paused),
    { success: d.paused ? 'Даг снят с паузы' : 'Даг поставлен на паузу' },
  )
  if (ok !== undefined)
    await load()
}

async function toggleAutoUpdate() {
  const d = dag.value
  if (!d)
    return
  const ok = await action.run(
    () => setDagAutoUpdate(d.name, !d.auto_update),
    { success: d.auto_update ? 'Авто-обновление выключено' : 'Авто-обновление включено' },
  )
  if (ok !== undefined)
    await load()
}

function formatSeconds(sec: number): string {
  if (!sec)
    return '—'
  if (sec % 3600 === 0)
    return `${sec / 3600}ч`
  if (sec % 60 === 0)
    return `${sec / 60}м`
  return `${sec}с`
}

function formatResources(t: DagTask): string {
  const r = t.resources
  if (!r)
    return '—'
  const parts: string[] = []
  if (r.cpu_request || r.cpu_limit)
    parts.push(`cpu ${r.cpu_request || '—'}/${r.cpu_limit || '—'}`)
  if (r.memory_request || r.memory_limit)
    parts.push(`mem ${r.memory_request || '—'}/${r.memory_limit || '—'}`)
  return parts.length ? parts.join(' · ') : '—'
}

const taskColumns: TableColumn<DagTask>[] = [
  { accessorKey: 'name', header: 'Таск' },
  { id: 'depends_on', header: 'Зависимости' },
  { accessorKey: 'retries', header: 'Ретраи' },
  { id: 'timeout', header: 'Таймаут' },
  { id: 'resources', header: 'Ресурсы (req/lim)' },
  { accessorKey: 'pool', header: 'Пул' },
  { accessorKey: 'priority', header: 'Приоритет' },
  { id: 'injections', header: 'Инъекции (env)' },
]

const regColumns: TableColumn<DagRegistration>[] = [
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'source', header: 'Источник' },
  { accessorKey: 'image', header: 'Образ' },
  { accessorKey: 'created_at', header: 'Создана' },
  { accessorKey: 'finished_at', header: 'Завершена' },
]

const tableUi = { td: 'whitespace-normal' }

function regStatusColor(status: DagRegistration['status']) {
  switch (status) {
    case 'success': return 'success' as const
    case 'failed': return 'error' as const
    case 'running': return 'info' as const
    default: return 'neutral' as const
  }
}

function regStatusLabel(status: DagRegistration['status']): string {
  switch (status) {
    case 'pending': return 'в очереди'
    case 'running': return 'выполняется'
    case 'success': return 'успех'
    case 'failed': return 'провал'
  }
}
</script>

<template>
  <UDashboardPanel id="dag-details">
    <template #header>
      <UDashboardNavbar :title="dagName">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/dags" />
        </template>
        <template #right>
          <UBadge v-if="dag?.paused" color="warning" variant="subtle" size="lg">пауза</UBadge>
          <UBadge v-if="isUpdating" color="info" variant="subtle" size="lg">
            <UIcon name="i-lucide-loader-circle" class="animate-spin" />
            обновляется
          </UBadge>
          <UTooltip v-if="canManage" text="Запустить ран">
            <UButton icon="i-lucide-play" color="primary" variant="ghost" @click="triggerTarget = dag" />
          </UTooltip>
          <UTooltip v-if="dag?.schedule && canManage" text="Backfill за период">
            <UButton icon="i-lucide-calendar-clock" color="secondary" variant="ghost" @click="backfillTarget = dag" />
          </UTooltip>
          <UTooltip v-if="canManage" text="Расписание">
            <UButton icon="i-lucide-alarm-clock" color="neutral" variant="ghost" @click="scheduleTarget = dag" />
          </UTooltip>
          <UTooltip v-if="isAdmin" :text="dag?.auto_update ? 'Выключить авто-обновление образа' : 'Включить авто-обновление образа'">
            <UButton
              icon="i-lucide-refresh-ccw-dot"
              :color="dag?.auto_update ? 'info' : 'neutral'"
              variant="ghost"
              @click="toggleAutoUpdate"
            />
          </UTooltip>
          <UTooltip v-if="canManage" :text="dag?.paused ? 'Снять с паузы' : 'Поставить на паузу'">
            <UButton
              :icon="dag?.paused ? 'i-lucide-play-circle' : 'i-lucide-pause-circle'"
              color="warning"
              variant="ghost"
              @click="togglePaused"
            />
          </UTooltip>
          <UTooltip v-if="isAdmin" text="Удалить даг">
            <UButton icon="i-lucide-trash-2" color="error" variant="ghost" @click="deleteTarget = dag" />
          </UTooltip>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load()" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" />

      <template v-if="dag">
        <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm lg:grid-cols-4">
          <div>
            <div class="text-muted">Расписание</div>
            <div v-if="dag.schedule">
              <span class="font-mono">{{ dag.schedule }}</span>
              <UBadge v-if="dag.catchup" color="info" variant="subtle" size="sm" class="ml-1.5">catchup</UBadge>
            </div>
            <div v-else class="text-muted">— (запуск вручную)</div>
          </div>
          <div>
            <div class="text-muted">Следующий запуск</div>
            <div>{{ dag.next_run_at && !dag.paused ? formatDateTime(dag.next_run_at) : '—' }}</div>
          </div>
          <div>
            <div class="text-muted">SDK</div>
            <div>{{ dag.sdk_version }}</div>
          </div>
          <div>
            <div class="text-muted">Лимит активных ранов</div>
            <div>{{ dag.max_active_runs || 'без лимита' }}</div>
          </div>
          <div class="col-span-2">
            <div class="text-muted">Образ</div>
            <CopyText :text="dag.image" mono />
          </div>
          <div class="col-span-2">
            <div class="text-muted">Digest</div>
            <CopyText :text="dag.image_digest" mono />
          </div>
          <div>
            <div class="text-muted">Зарегистрирован</div>
            <div>{{ formatDateTime(dag.created_at) }}</div>
          </div>
          <div>
            <div class="text-muted">Обновлён</div>
            <div>{{ formatDateTime(dag.modified_at) }}</div>
          </div>
          <div class="col-span-2 flex items-end gap-6">
            <NuxtLink :to="`/runs?dag_name=${encodeURIComponent(dag.name)}`" class="text-primary hover:underline">
              Раны дага →
            </NuxtLink>
            <NuxtLink :to="`/env?dag_name=${encodeURIComponent(dag.name)}`" class="text-primary hover:underline">
              Переменные и секреты дага →
            </NuxtLink>
          </div>
        </div>

        <section>
          <h3 class="mb-2 font-semibold text-highlighted">Граф</h3>
          <RunDagGraph :manifest-tasks="dag.tasks" />
        </section>

        <section>
          <h3 class="mb-2 font-semibold text-highlighted">Таски</h3>
          <UTable :data="dag.tasks" :columns="taskColumns" :ui="tableUi">
            <template #name-cell="{ row }">
              <span class="font-medium">{{ row.original.name }}</span>
            </template>
            <template #depends_on-cell="{ row }">
              <div v-if="row.original.depends_on.length" class="flex flex-wrap gap-1">
                <UBadge
                  v-for="dep in row.original.depends_on"
                  :key="dep.task"
                  color="neutral"
                  variant="subtle"
                  size="sm"
                >
                  {{ dep.task }}{{ dep.streamed ? ' (stream)' : '' }}
                </UBadge>
              </div>
              <span v-else class="text-muted">—</span>
            </template>
            <template #retries-cell="{ row }">
              <template v-if="row.original.retries">
                {{ row.original.retries }}
                <span v-if="row.original.retry_delay_sec" class="text-xs text-muted">
                  · пауза {{ formatSeconds(row.original.retry_delay_sec) }}
                </span>
              </template>
              <span v-else class="text-muted">—</span>
            </template>
            <template #timeout-cell="{ row }">
              {{ formatSeconds(row.original.timeout_sec) }}
            </template>
            <template #resources-cell="{ row }">
              <span class="font-mono text-xs">{{ formatResources(row.original) }}</span>
            </template>
            <template #pool-cell="{ row }">
              {{ row.original.pool || 'default' }}
            </template>
            <template #priority-cell="{ row }">
              {{ row.original.priority || '—' }}
            </template>
            <template #injections-cell="{ row }">
              <div v-if="row.original.secrets?.length || row.original.variables?.length" class="flex flex-wrap gap-1">
                <UTooltip v-for="s in row.original.secrets" :key="`s-${s.env}`" :text="`секрет ${s.secret}`">
                  <UBadge color="warning" variant="subtle" size="sm" class="font-mono">
                    <UIcon name="i-lucide-key-round" class="size-3" />
                    {{ s.env }}
                  </UBadge>
                </UTooltip>
                <UTooltip v-for="v in row.original.variables" :key="`v-${v.env}`" :text="`переменная ${v.variable}`">
                  <UBadge color="neutral" variant="subtle" size="sm" class="font-mono">
                    <UIcon name="i-lucide-variable" class="size-3" />
                    {{ v.env }}
                  </UBadge>
                </UTooltip>
              </div>
              <span v-else class="text-muted">—</span>
            </template>
          </UTable>
        </section>

        <section v-if="registrations.length">
          <h3 class="mb-2 font-semibold text-highlighted">История регистраций</h3>
          <UTable :data="registrations" :columns="regColumns" :ui="tableUi">
            <template #status-cell="{ row }">
              <div class="flex flex-col gap-0.5">
                <UBadge :color="regStatusColor(row.original.status)" variant="subtle" class="w-fit">
                  {{ regStatusLabel(row.original.status) }}
                </UBadge>
                <span v-if="row.original.error" class="text-xs text-error">{{ row.original.error }}</span>
              </div>
            </template>
            <template #source-cell="{ row }">
              {{ row.original.source === 'auto' ? 'авто (digest)' : 'вручную' }}
            </template>
            <template #image-cell="{ row }">
              <span class="font-mono text-xs">{{ row.original.image }}</span>
            </template>
            <template #created_at-cell="{ row }">
              {{ formatDateTime(row.original.created_at) }}
            </template>
            <template #finished_at-cell="{ row }">
              {{ formatDateTime(row.original.finished_at) }}
            </template>
          </UTable>
        </section>
      </template>

      <DagTriggerModal :dag="triggerTarget" @close="triggerTarget = null" />
      <DagBackfillModal :dag="backfillTarget" @close="backfillTarget = null" />
      <DagScheduleModal :dag="scheduleTarget" @close="scheduleTarget = null" @saved="onScheduleSaved" />
      <DagDeleteModal :dag="deleteTarget" @close="deleteTarget = null" @deleted="onDeleted" />
    </template>
  </UDashboardPanel>
</template>
