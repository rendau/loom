<script setup lang="ts">
import type { TableColumn, TabsItem } from '@nuxt/ui'
import type { DagTask } from '~/types/dag'
import type { Attempt, TaskInstance, TaskStatus, TaskValue } from '~/types/run'
import type { RunEnvBinding } from '~/utils/runenv'

// Инспектор таска — единица диагностики (design/02): всё, что принадлежит
// таску, в одном месте — лог, попытки, значения, окружение. Живёт на
// странице рана под списком тасков; выбор и таба — в URL (?task=&tab=).

const props = defineProps<{
  runId: string
  ti: TaskInstance
  manifestTask?: DagTask
  attempts: Attempt[] // попытки этого таска (по возрастанию attempt)
  values: TaskValue[] // значения этого таска
  bindings: RunEnvBinding[] // резолвнутые env-привязки этого таска
  // статусы зависимостей (для пустого состояния pending/queued)
  deps: Array<{ task: string, streamed: boolean, status?: TaskStatus }>
  canRetry: boolean
}>()

const emit = defineEmits<{ close: [], retry: [] }>()

const tab = defineModel<string>('tab', { default: 'log' })

const started = computed(() => props.ti.attempt >= 1)

const tabItems = computed<TabsItem[]>(() => {
  const items: TabsItem[] = [{ label: 'Лог', value: 'log', icon: 'i-lucide-scroll-text' }]
  if (props.attempts.length > 1)
    items.push({ label: 'Попытки', value: 'attempts', icon: 'i-lucide-rotate-ccw', badge: props.attempts.length })
  if (props.values.length > 0)
    items.push({ label: 'Значения', value: 'values', icon: 'i-lucide-braces', badge: props.values.length })
  if (props.bindings.length > 0)
    items.push({ label: 'Env', value: 'env', icon: 'i-lucide-key-round', badge: props.bindings.length })
  return items
})

// если выбранная таба исчезла (сменился таск) — назад на лог
watch(tabItems, (items) => {
  if (!items.some(i => i.value === tab.value))
    tab.value = 'log'
})

// ── лог: просмотр любой попытки, по умолчанию — текущая ─────

const logAttempt = ref(props.ti.attempt)
watch(() => [props.ti.task, props.ti.attempt], () => {
  logAttempt.value = props.ti.attempt
})

const attemptItems = computed(() =>
  props.attempts.map(a => ({ label: `попытка ${a.attempt}`, value: a.attempt })))

// follow — для живой текущей попытки
const followLog = computed(() =>
  logAttempt.value === props.ti.attempt
  && (props.ti.status === 'running' || props.ti.status === 'starting'))

const fullscreenTo = computed(() =>
  `/runs/${encodeURIComponent(props.runId)}/log?task=${encodeURIComponent(props.ti.task)}`
  + `&attempt=${logAttempt.value}&follow=${followLog.value ? '1' : '0'}`)

// ── попытки ─────────────────────────────────────────────

const attemptColumns: TableColumn<Attempt>[] = [
  { accessorKey: 'attempt', header: '№' },
  { accessorKey: 'status', header: 'Статус' },
  { accessorKey: 'created_at', header: 'Создана' },
  { id: 'duration', header: 'Длительность' },
  { id: 'memory', header: 'Пик памяти' },
  { id: 'exit', header: 'Исход' },
  { id: 'actions', header: '' },
]

function openAttemptLog(a: Attempt) {
  logAttempt.value = a.attempt
  tab.value = 'log'
}

// ── значения ────────────────────────────────────────────

const valueColumns: TableColumn<TaskValue>[] = [
  { accessorKey: 'key', header: 'Ключ' },
  { accessorKey: 'value', header: 'Значение' },
  { accessorKey: 'modified_at', header: 'Обновлено' },
]

function formatValue(v: unknown): string {
  const s = JSON.stringify(v)
  return s.length > 200 ? `${s.slice(0, 199)}…` : s
}

// ── высота контента: ручка ресайза сверху панели ────────
// (риск «45vh мало» из design/07 — вместо фикса высота тянется и
// запоминается; клавиши ↑/↓ на сфокусированной ручке — то же самое)

const HEIGHT_KEY = 'loom-inspector-height'
const MIN_H = 240
const MAX_H = 800

function clampH(v: number): number {
  return Math.min(MAX_H, Math.max(MIN_H, v))
}

const bodyHeight = ref(clampH(Number(localStorage.getItem(HEIGHT_KEY)) || 420))
watch(bodyHeight, v => localStorage.setItem(HEIGHT_KEY, String(v)))

function startResize(e: PointerEvent) {
  const startY = e.clientY
  const startH = bodyHeight.value
  const move = (ev: PointerEvent) => {
    // ручка сверху: тянем вверх — панель выше
    bodyHeight.value = clampH(startH + (startY - ev.clientY))
  }
  const up = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
  e.preventDefault()
}

function resizeByKey(e: KeyboardEvent) {
  if (e.key === 'ArrowUp')
    bodyHeight.value = clampH(bodyHeight.value + 40)
  else if (e.key === 'ArrowDown')
    bodyHeight.value = clampH(bodyHeight.value - 40)
  else
    return
  e.preventDefault()
}

// исход последней попытки — в заголовок (при одной попытке отдельная
// таблица не нужна)
const lastAttempt = computed(() => props.attempts[props.attempts.length - 1])

const exitInfo = computed(() => {
  const a = lastAttempt.value
  if (!a)
    return ''
  const parts: string[] = []
  if (a.exit_code !== undefined && a.exit_code !== null)
    parts.push(`exit code ${a.exit_code}`)
  if (a.exit_reason)
    parts.push(a.exit_reason)
  return parts.join(' · ')
})
</script>

<template>
  <section class="flex min-h-0 flex-col overflow-hidden rounded-lg border border-default">
    <!-- ручка ресайза высоты контента -->
    <div
      role="separator"
      aria-orientation="horizontal"
      aria-label="Высота инспектора (перетащить или стрелки ↑/↓)"
      tabindex="0"
      class="group flex h-2 shrink-0 cursor-row-resize items-center justify-center border-b border-default bg-muted/30 hover:bg-accented/50 focus-visible:bg-accented/50 focus-visible:outline-none"
      style="touch-action: none"
      @pointerdown="startResize"
      @keydown="resizeByKey"
    >
      <div class="h-0.5 w-10 rounded-full bg-accented group-hover:bg-dimmed" />
    </div>

    <!-- заголовок: идентичность таска + исход + действия -->
    <div class="flex flex-wrap items-center gap-2 border-b border-default px-3 py-2">
      <span class="font-medium text-highlighted">{{ ti.task }}</span>
      <StatusBadge kind="task" :status="ti.status" size="sm" />
      <span v-if="started" class="text-xs text-muted">
        попытка {{ ti.attempt }}<template v-if="attempts.length > 1"> из {{ attempts.length }}</template>
      </span>
      <span v-if="exitInfo" class="font-mono text-xs" :class="ti.status === 'failed' ? 'text-error' : 'text-muted'">
        {{ exitInfo }}
      </span>
      <span class="ms-auto flex items-center gap-1">
        <UButton
          v-if="canRetry"
          icon="i-lucide-rotate-ccw"
          label="Ретрай"
          size="xs"
          color="neutral"
          variant="soft"
          @click="emit('retry')"
        />
        <UButton
          icon="i-lucide-x"
          size="xs"
          color="neutral"
          variant="ghost"
          aria-label="Закрыть инспектор"
          @click="emit('close')"
        />
      </span>
    </div>

    <!-- таск ещё не запускался: причина ожидания вместо пустых табов -->
    <div v-if="!started" class="p-4 text-sm text-muted">
      <p>Таск ещё не запускался{{ ti.status === 'queued' ? ' — в очереди' : '' }}.</p>
      <template v-if="deps.length">
        <p class="mt-2 text-xs">Зависимости:</p>
        <ul class="mt-1 space-y-1">
          <li v-for="dep in deps" :key="dep.task" class="flex items-center gap-2">
            <span class="font-medium text-default">{{ dep.task }}</span>
            <StatusBadge v-if="dep.status" kind="task" :status="dep.status" size="sm" />
            <span v-if="dep.streamed" class="text-xs text-muted">(stream, ко-старт)</span>
          </li>
        </ul>
      </template>
    </div>

    <template v-else>
      <div class="flex flex-wrap items-center gap-2 border-b border-default px-3 py-1.5">
        <UTabs
          v-model="tab"
          :items="tabItems"
          :content="false"
          color="neutral"
          variant="pill"
          size="sm"
        />
        <span v-if="tab === 'log'" class="ms-auto flex items-center gap-2">
          <USelect
            v-if="attempts.length > 1"
            v-model="logAttempt"
            :items="attemptItems"
            value-key="value"
            size="xs"
            class="w-32"
          />
          <UButton
            :to="fullscreenTo"
            icon="i-lucide-maximize-2"
            label="На весь экран"
            color="neutral"
            variant="ghost"
            size="xs"
          />
        </span>
      </div>

      <!-- лог: ремоунт на смену таска/попытки, иначе вьювер продолжит стрим -->
      <RunLogViewer
        v-if="tab === 'log'"
        :key="`${runId}/${ti.task}/${logAttempt}`"
        :run-id="runId"
        :task="ti.task"
        :attempt="logAttempt"
        :follow="followLog"
        :style="{ height: `${bodyHeight}px` }"
      />

      <div v-else class="min-h-0 overflow-auto p-3" :style="{ maxHeight: `${bodyHeight}px` }">
        <UTable v-if="tab === 'attempts'" :data="attempts" :columns="attemptColumns" :ui="denseTableUi">
          <template #status-cell="{ row }">
            <StatusBadge kind="attempt" :status="row.original.status" size="sm" />
          </template>
          <template #created_at-cell="{ row }">
            <RelativeTime :time="row.original.created_at" />
          </template>
          <template #duration-cell="{ row }">
            {{ formatDuration(row.original.started_at, row.original.finished_at) }}
          </template>
          <template #memory-cell="{ row }">
            {{ formatBytes(row.original.peak_memory_bytes) }}
          </template>
          <template #exit-cell="{ row }">
            <span class="font-mono text-xs">
              <template v-if="row.original.exit_code !== undefined && row.original.exit_code !== null">
                code {{ row.original.exit_code }}
              </template>
              <template v-if="row.original.exit_reason">
                · {{ row.original.exit_reason }}
              </template>
            </span>
          </template>
          <template #actions-cell="{ row }">
            <div class="flex justify-end">
              <UTooltip text="Лог попытки">
                <UButton
                  icon="i-lucide-scroll-text"
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  aria-label="Лог попытки"
                  @click="openAttemptLog(row.original)"
                />
              </UTooltip>
            </div>
          </template>
        </UTable>

        <UTable v-else-if="tab === 'values'" :data="values" :columns="valueColumns" :ui="denseTableUi">
          <template #key-cell="{ row }">
            <span class="font-mono text-xs font-medium">{{ row.original.key }}</span>
          </template>
          <template #value-cell="{ row }">
            <div class="max-w-xl break-all font-mono text-xs" :title="formatValue(row.original.value)">
              {{ formatValue(row.original.value) }}
            </div>
          </template>
          <template #modified_at-cell="{ row }">
            <RelativeTime :time="row.original.modified_at" />
          </template>
        </UTable>

        <RunEnvTable v-else-if="tab === 'env'" :bindings="bindings" />
      </div>
    </template>
  </section>
</template>
