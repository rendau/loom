<script setup lang="ts">
import { useVirtualizer } from '@tanstack/vue-virtual'
import { readTaskLog } from '~/api/log.api'
import type { LogStreamStatus } from '~/api/log.api'
import type { TaskLogEntry } from '~/types/log'
import type { LogContext, LogLevel, ParsedLogLine } from '~/utils/logparse'

// Просмотр лога попытки: виртуализированный список с разбором logfmt/JSON,
// ANSI-раскраской, фильтрами по уровню/источнику, поиском и live-follow с
// реконнектом (докачка с after_seq — без потерь и дублей).

const props = defineProps<{
  runId: string
  task: string
  attempt: number
  follow: boolean
}>()

interface Row {
  entry: TaskLogEntry
  parsed: ParsedLogLine
}

// контекст просмотра: пары dag/run_id/task/attempt, повторяющие его, из
// строк убираются — они и так в заголовке
const logContext = computed<LogContext>(() => ({
  runId: props.runId,
  task: props.task,
  attempt: props.attempt,
}))

const rows = shallowRef<Row[]>([])
const errorText = ref('')
const streaming = ref(false)
const streamStatus = ref<LogStreamStatus>('connected')
const following = ref(props.follow)

let ctrl: AbortController | undefined
let received = 0

async function start(continueFrom = 0) {
  ctrl?.abort()
  const myCtrl = new AbortController()
  ctrl = myCtrl

  if (continueFrom === 0) {
    rows.value = []
    received = 0
  }
  errorText.value = ''
  streaming.value = true
  streamStatus.value = 'connected'

  try {
    await readTaskLog(
      {
        runId: props.runId,
        task: props.task,
        attempt: props.attempt,
        follow: following.value,
        afterSeq: continueFrom,
      },
      (batch) => {
        received += batch.length
        rows.value = [...rows.value, ...batch.map(entry => ({ entry, parsed: parseLogLine(entry.line, logContext.value) }))]
        scrollDownIfPinned()
      },
      myCtrl.signal,
      (status) => {
        streamStatus.value = status
      },
    )
  }
  catch (error) {
    if (!myCtrl.signal.aborted)
      errorText.value = error instanceof Error ? error.message : String(error)
  }
  finally {
    if (ctrl === myCtrl)
      streaming.value = false
  }
}

// переключение follow не перечитывает лог: продолжаем с полученного места
function onFollowToggle(value: boolean) {
  following.value = value
  start(value ? received : 0)
}

watch(() => [props.runId, props.task, props.attempt], () => {
  following.value = props.follow
  start()
})

onMounted(() => start())
onBeforeUnmount(() => ctrl?.abort())

// ── фильтры и поиск ─────────────────────────────────────

const levelFilter = ref<Array<LogLevel | 'none'>>([])
const sourceFilter = ref<string[]>([])
const search = ref('')
const wrap = ref(true)
const ansiColors = ref(true)

const levelItems: Array<{ label: string, value: LogLevel | 'none' }> = [
  { label: 'debug', value: 'debug' },
  { label: 'info', value: 'info' },
  { label: 'warn', value: 'warn' },
  { label: 'error', value: 'error' },
  { label: 'без уровня', value: 'none' },
]

const sourceItems = [
  { label: 'log', value: 'TASK_LOG_SOURCE_LOG' },
  { label: 'stdout', value: 'TASK_LOG_SOURCE_STDOUT' },
  { label: 'stderr', value: 'TASK_LOG_SOURCE_STDERR' },
  { label: 'server', value: 'TASK_LOG_SOURCE_SERVER' },
]

const filteredRows = computed(() => {
  const levels = new Set(levelFilter.value)
  const sources = new Set(sourceFilter.value)
  const query = search.value.trim().toLowerCase()

  return rows.value.filter((row) => {
    if (levels.size > 0 && !levels.has(row.parsed.level ?? 'none'))
      return false
    if (sources.size > 0 && !sources.has(row.entry.source))
      return false
    if (query && !row.parsed.clean.toLowerCase().includes(query))
      return false
    return true
  })
})

// ── виртуализация и автоскролл ──────────────────────────

const scrollRef = ref<HTMLElement | null>(null)
const pinned = ref(true) // прокручен к низу — новые строки автоскроллятся

const virtualizer = useVirtualizer(computed(() => ({
  count: filteredRows.value.length,
  getScrollElement: () => scrollRef.value,
  estimateSize: () => 22,
  overscan: 20,
})))

const virtualRows = computed(() => virtualizer.value.getVirtualItems())
const totalSize = computed(() => virtualizer.value.getTotalSize())

function onScroll() {
  const el = scrollRef.value
  if (!el)
    return
  pinned.value = el.scrollTop + el.clientHeight >= el.scrollHeight - 40
}

function scrollDownIfPinned() {
  if (pinned.value)
    scrollToBottom()
}

function scrollToBottom() {
  nextTick(() => {
    const el = scrollRef.value
    if (el)
      el.scrollTop = el.scrollHeight
    pinned.value = true
  })
}

// ── раскрытие JSON-строк ────────────────────────────────

const expanded = ref(new Set<TaskLogEntry>())

function toggleExpand(row: Row) {
  if (row.parsed.kind !== 'json')
    return
  const next = new Set(expanded.value)
  if (next.has(row.entry))
    next.delete(row.entry)
  else
    next.add(row.entry)
  expanded.value = next
}

function prettyJson(row: Row): string {
  return JSON.stringify(row.parsed.json, null, 2)
}

// ── копирование и скачивание ────────────────────────────

const toast = useToast()

async function copyAll() {
  const text = filteredRows.value.map(r => r.parsed.clean).join('\n')
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ title: `Скопировано строк: ${filteredRows.value.length}`, color: 'success' })
  }
  catch {
    toast.add({ title: 'Буфер обмена недоступен', color: 'error' })
  }
}

function download() {
  const text = rows.value.map(r => `${formatTimestampMs(r.entry.ts_unix_ms)} ${logSourceLabel(r.entry.source)} ${r.parsed.clean}`).join('\n')
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${props.runId}_${props.task}_${props.attempt}.log`
  a.click()
  URL.revokeObjectURL(url)
}

defineExpose({ restart: () => start() })
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <!-- тулбар -->
    <div class="flex flex-wrap items-center gap-2 border-b border-default p-2">
      <UInput
        v-model="search"
        icon="i-lucide-search"
        placeholder="Поиск"
        size="sm"
        class="w-48"
      />
      <USelect
        v-model="levelFilter"
        :items="levelItems"
        value-key="value"
        multiple
        placeholder="Уровень"
        size="sm"
        class="w-36"
      />
      <USelect
        v-model="sourceFilter"
        :items="sourceItems"
        value-key="value"
        multiple
        placeholder="Источник"
        size="sm"
        class="w-36"
      />
      <USwitch v-model="wrap" label="Перенос" size="sm" />
      <USwitch v-model="ansiColors" label="ANSI" size="sm" />

      <span class="ms-auto flex items-center gap-2">
        <span class="text-xs text-muted">{{ filteredRows.length }} / {{ rows.length }}</span>
        <UBadge v-if="streaming && streamStatus === 'connected'" color="info" variant="subtle" size="sm">
          <UIcon name="i-lucide-radio" class="size-3 animate-pulse" />
          live
        </UBadge>
        <UBadge v-else-if="streaming" color="warning" variant="subtle" size="sm">
          <UIcon name="i-lucide-loader-circle" class="size-3 animate-spin" />
          реконнект…
        </UBadge>
        <USwitch :model-value="following" label="Follow" size="sm" @update:model-value="onFollowToggle" />
        <UTooltip text="Скопировать отфильтрованное">
          <UButton icon="i-lucide-copy" size="xs" color="neutral" variant="ghost" @click="copyAll" />
        </UTooltip>
        <UTooltip text="Скачать .log">
          <UButton icon="i-lucide-download" size="xs" color="neutral" variant="ghost" @click="download" />
        </UTooltip>
      </span>
    </div>

    <!-- строки -->
    <div class="relative min-h-0 flex-1">
      <div ref="scrollRef" class="h-full overflow-auto bg-elevated font-mono text-xs leading-snug" @scroll.passive="onScroll">
        <UAlert v-if="errorText" color="error" variant="subtle" :title="errorText" class="m-2 w-auto font-sans" />

        <div v-if="!errorText" :style="{ height: `${totalSize}px`, position: 'relative' }" class="w-full">
          <div
            v-for="vRow in virtualRows"
            :key="vRow.key as number"
            :ref="(el) => { if (el) virtualizer.measureElement(el as Element) }"
            :data-index="vRow.index"
            :style="{ position: 'absolute', top: 0, left: 0, width: '100%', transform: `translateY(${vRow.start}px)` }"
          >
            <template v-if="filteredRows[vRow.index]">
              <div
                class="flex gap-2 px-3 py-0.5 hover:bg-accented/40"
                :class="filteredRows[vRow.index]!.parsed.kind === 'json' ? 'cursor-pointer' : ''"
                @click="toggleExpand(filteredRows[vRow.index]!)"
              >
                <span class="shrink-0 text-dimmed">{{ formatTimestampMs(filteredRows[vRow.index]!.entry.ts_unix_ms) }}</span>
                <span class="w-7 shrink-0" :class="logSourceClass(filteredRows[vRow.index]!.entry.source)">
                  {{ logSourceLabel(filteredRows[vRow.index]!.entry.source) }}
                </span>
                <span v-if="filteredRows[vRow.index]!.parsed.level" class="w-11 shrink-0">
                  <UBadge :color="logLevelColor(filteredRows[vRow.index]!.parsed.level)" variant="subtle" size="sm" class="px-1 py-0 text-[10px]">
                    {{ filteredRows[vRow.index]!.parsed.level }}
                  </UBadge>
                </span>

                <!-- содержимое строки -->
                <span class="min-w-0 flex-1" :class="wrap ? 'whitespace-pre-wrap break-words' : 'truncate whitespace-pre'">
                  <template v-if="filteredRows[vRow.index]!.parsed.kind === 'logfmt'">
                    <span class="text-highlighted">{{ filteredRows[vRow.index]!.parsed.msg }}</span>
                    <span v-for="[k, v] in filteredRows[vRow.index]!.parsed.fields" :key="k" class="ml-2 text-muted">
                      {{ k }}=<span class="text-default">{{ v }}</span>
                    </span>
                  </template>
                  <template v-else-if="filteredRows[vRow.index]!.parsed.kind === 'json'">
                    <UIcon
                      :name="expanded.has(filteredRows[vRow.index]!.entry) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                      class="mr-1 inline-block size-3 text-muted"
                    />
                    <span v-if="filteredRows[vRow.index]!.parsed.msg" class="text-highlighted">{{ filteredRows[vRow.index]!.parsed.msg }}</span>
                    <span class="ml-1 text-muted">{{ expanded.has(filteredRows[vRow.index]!.entry) ? '' : filteredRows[vRow.index]!.parsed.clean }}</span>
                  </template>
                  <template v-else-if="ansiColors && filteredRows[vRow.index]!.parsed.segments">
                    <span
                      v-for="(seg, si) in filteredRows[vRow.index]!.parsed.segments"
                      :key="si"
                      :class="[seg.colorClass, seg.bold ? 'font-bold' : '']"
                    >{{ seg.text }}</span>
                  </template>
                  <template v-else>
                    {{ filteredRows[vRow.index]!.parsed.clean }}
                  </template>
                </span>
              </div>

              <!-- развёрнутый JSON -->
              <pre
                v-if="expanded.has(filteredRows[vRow.index]!.entry)"
                class="mx-3 mb-1 overflow-x-auto rounded-md border border-default bg-muted/30 p-2"
              >{{ prettyJson(filteredRows[vRow.index]!) }}</pre>
            </template>
          </div>
        </div>

        <div v-if="!streaming && !errorText && rows.length === 0" class="p-4 text-muted">
          Лог пуст.
        </div>
      </div>

      <!-- кнопка «вниз», когда пользователь отмотал вверх -->
      <UButton
        v-if="!pinned"
        icon="i-lucide-arrow-down-to-line"
        color="neutral"
        variant="solid"
        size="sm"
        class="absolute bottom-3 right-4 shadow-lg"
        @click="scrollToBottom"
      />
    </div>
  </div>
</template>
