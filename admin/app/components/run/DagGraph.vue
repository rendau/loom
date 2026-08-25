<script setup lang="ts">
import type { DagTask } from '~/types/dag'
import type { TaskInstance, TaskStatus } from '~/types/run'

// Граф дага со статусами тасков. Раскладка слоистая: колонка таска — его
// ранг (длиннейший путь от корня), порядок внутри колонки — барицентр
// зависимостей. Рёбра — кривые Безье, стримовые (ко-старт) — пунктиром.
// Клик по таску выбирает его (инспектор на странице рана). Без tasks
// (карточка дага до запуска) — нейтральная раскраска, узлы некликабельны.
//
// На странице рана граф заменяет таблицу тасков: в узле — статус, номер
// попытки, длительность, пик памяти и кнопка ретрая.

const props = defineProps<{
  manifestTasks: DagTask[] // структура графа из снапшота манифеста рана
  tasks?: TaskInstance[] // статусы task instance'ов; нет — режим «схема дага»
  selected?: string // выделенный таск (синхронно с инспектором)
  compact?: boolean // ограниченная высота (карточка дага: граф — обзор)
  peakByTask?: Record<string, number> // пик памяти текущей попытки таска
  retryTasks?: string[] // таски, которые можно отправить на ретрай
}>()

const emit = defineEmits<{
  select: [task: TaskInstance]
  retry: [task: TaskInstance]
}>()

const NODE_W = 208
const NODE_H = 76
const GAP_X = 56
const GAP_Y = 20
const PAD = 8

interface GraphNode {
  name: string
  x: number
  y: number
  ti?: TaskInstance
}

interface GraphEdge {
  from: string
  to: string
  streamed: boolean
  path: string
}

const layout = computed(() => {
  const byName = new Map(props.manifestTasks.map(t => [t.name, t]))
  const tiByName = new Map((props.tasks ?? []).map(t => [t.task, t]))

  // ранг = длиннейший путь от корня; seen — защита от цикла (сервер
  // валидирует ацикличность, но падать на битых данных не хотим)
  const rank = new Map<string, number>()
  function rankOf(name: string, seen: Set<string>): number {
    const cached = rank.get(name)
    if (cached !== undefined)
      return cached
    if (seen.has(name))
      return 0
    seen.add(name)
    const deps = byName.get(name)?.depends_on ?? []
    const r = deps.length ? Math.max(...deps.map(d => rankOf(d.task, seen))) + 1 : 0
    rank.set(name, r)
    return r
  }
  for (const t of props.manifestTasks)
    rankOf(t.name, new Set())

  // колонки по рангу
  const cols: string[][] = []
  for (const t of props.manifestTasks) {
    const r = rank.get(t.name) ?? 0
    ;(cols[r] ??= []).push(t.name)
  }

  // порядок внутри колонки — барицентр строк зависимостей (меньше пересечений)
  const rowOf = new Map<string, number>()
  cols.forEach((col, c) => {
    if (c > 0) {
      const center = (name: string) => {
        const rows = (byName.get(name)?.depends_on ?? [])
          .map(d => rowOf.get(d.task))
          .filter((v): v is number => v !== undefined)
        return rows.length ? rows.reduce((s, v) => s + v, 0) / rows.length : 0
      }
      col.sort((a, b) => center(a) - center(b))
    }
    col.forEach((name, i) => rowOf.set(name, i))
  })

  const maxRows = Math.max(1, ...cols.map(c => c.length))
  const height = maxRows * NODE_H + (maxRows - 1) * GAP_Y + PAD * 2
  const width = cols.length * NODE_W + (cols.length - 1) * GAP_X + PAD * 2

  // позиции: колонки слева направо, каждая отцентрована по вертикали
  const pos = new Map<string, { x: number, y: number }>()
  const nodes: GraphNode[] = []
  cols.forEach((col, c) => {
    const colHeight = col.length * NODE_H + (col.length - 1) * GAP_Y
    const top = PAD + (height - PAD * 2 - colHeight) / 2
    col.forEach((name, i) => {
      const p = { x: PAD + c * (NODE_W + GAP_X), y: top + i * (NODE_H + GAP_Y) }
      pos.set(name, p)
      nodes.push({ name, ...p, ti: tiByName.get(name) })
    })
  })

  const edges: GraphEdge[] = []
  for (const t of props.manifestTasks) {
    for (const d of t.depends_on ?? []) {
      const from = pos.get(d.task)
      const to = pos.get(t.name)
      if (!from || !to)
        continue
      const x1 = from.x + NODE_W
      const y1 = from.y + NODE_H / 2
      const x2 = to.x
      const y2 = to.y + NODE_H / 2
      const dx = Math.max(24, (x2 - x1) / 2)
      edges.push({
        from: d.task,
        to: t.name,
        streamed: d.streamed,
        path: `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`,
      })
    }
  }

  return { nodes, edges, width, height }
})

// Цвета — семантические CSS-токены Nuxt UI, те же смыслы, что у бейджей
// (см. taskStatusColor в utils/status.ts).
function statusVar(status?: TaskStatus): string {
  switch (status) {
    case 'queued': return 'var(--ui-secondary)'
    case 'starting':
    case 'running': return 'var(--ui-info)'
    case 'up_for_retry': return 'var(--ui-warning)'
    case 'success': return 'var(--ui-success)'
    case 'failed': return 'var(--ui-error)'
    // upstream_failed — следствие чужого провала: приглушаем, чтобы не
    // отвлекать от реально упавшего таска (design/06 §3)
    default: return 'var(--ui-border-accented)' // pending, upstream_failed, canceled
  }
}

function nodeFill(status?: TaskStatus): string {
  if (!status || status === 'pending' || status === 'upstream_failed' || status === 'canceled')
    return 'var(--ui-bg-elevated)'
  return `color-mix(in srgb, ${statusVar(status)} 10%, var(--ui-bg))`
}

function statusText(ti?: TaskInstance): string {
  if (!ti)
    return props.tasks ? '—' : ''
  const label = taskStatusLabel(ti.status)
  return ti.attempt > 1 ? `${label} · попытка ${ti.attempt}` : label
}

// живой тик — только для длительности выполняющихся тасков
const now = useTimeTick()

// метрики попытки: длительность и пик памяти (память приблизительна и у
// коротких попыток может отсутствовать)
function metricsText(ti?: TaskInstance): string {
  if (!ti || !ti.started_at)
    return ''
  const live = ti.status === 'running' || ti.status === 'starting'
  const parts = [formatDuration(ti.started_at, ti.finished_at, live ? now.value : undefined)]
  const peak = props.peakByTask?.[ti.task]
  if (peak !== undefined)
    parts.push(formatBytes(peak))
  return parts.join(' · ')
}

function truncate(name: string): string {
  return name.length > 22 ? `${name.slice(0, 21)}…` : name
}

function clickable(n: GraphNode): boolean {
  return !!n.ti
}

function retryable(n: GraphNode): boolean {
  return !!n.ti && (props.retryTasks?.includes(n.name) ?? false)
}

function onNodeClick(n: GraphNode) {
  if (clickable(n) && n.ti)
    emit('select', n.ti)
}

function onRetryClick(n: GraphNode) {
  if (n.ti)
    emit('retry', n.ti)
}
</script>

<template>
  <div>
    <div class="overflow-auto rounded-lg border border-default p-2" :class="compact ? 'max-h-[280px]' : 'max-h-[420px]'">
      <svg :width="layout.width" :height="layout.height" class="block">
        <defs>
          <marker
            id="dag-arrow"
            viewBox="0 0 8 8"
            refX="7"
            refY="4"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 8 4 L 0 8 z" fill="var(--ui-border-accented)" />
          </marker>
          <!-- подписи узлов клипаются по ширине узла: truncate по символам —
               эвристика, широкий шрифт вылезал бы за рамку -->
          <clipPath id="dag-node-text-clip">
            <rect x="0" y="0" :width="NODE_W - 40" :height="NODE_H" />
          </clipPath>
        </defs>

        <path
          v-for="e in layout.edges"
          :key="`${e.from}->${e.to}`"
          :d="e.path"
          fill="none"
          stroke="var(--ui-border-accented)"
          stroke-width="1.5"
          :stroke-dasharray="e.streamed ? '6 4' : undefined"
          marker-end="url(#dag-arrow)"
        />

        <g
          v-for="n in layout.nodes"
          :key="n.name"
          :transform="`translate(${n.x}, ${n.y})`"
          :class="clickable(n) ? 'cursor-pointer' : ''"
          :role="clickable(n) ? 'button' : undefined"
          :tabindex="clickable(n) ? 0 : undefined"
          :aria-label="clickable(n) ? `Таск ${n.name}` : undefined"
          @click="onNodeClick(n)"
          @keydown.enter.prevent="onNodeClick(n)"
        >
          <title>{{ n.name }}{{ statusText(n.ti) ? ` — ${statusText(n.ti)}` : '' }}</title>
          <!-- выделение выбранного — нейтральная внешняя рамка: цвет рамки
               узла остаётся статусным (primary совпадает с success-зелёным
               и на failed-узле читался бы как «успех») -->
          <rect
            v-if="n.name === selected"
            x="-3.5"
            y="-3.5"
            :width="NODE_W + 7"
            :height="NODE_H + 7"
            rx="11"
            fill="none"
            stroke="var(--ui-text-highlighted)"
            stroke-width="1.5"
          />
          <rect
            :width="NODE_W"
            :height="NODE_H"
            rx="8"
            :fill="nodeFill(n.ti?.status)"
            :stroke="statusVar(n.ti?.status)"
            stroke-width="1.5"
            :class="n.ti?.status === 'running' || n.ti?.status === 'starting' ? 'animate-pulse' : ''"
          />
          <g clip-path="url(#dag-node-text-clip)">
            <text x="12" :y="statusText(n.ti) ? 24 : NODE_H / 2 + 5" font-size="13" font-weight="500" fill="var(--ui-text-highlighted)">
              {{ truncate(n.name) }}
            </text>
            <text v-if="statusText(n.ti)" x="12" y="44" font-size="11" :fill="n.ti ? statusVar(n.ti.status) : 'var(--ui-text-muted)'">
              {{ statusText(n.ti) }}
            </text>
            <text v-if="metricsText(n.ti)" x="12" y="62" font-size="11" fill="var(--ui-text-muted)">
              {{ metricsText(n.ti) }}
            </text>
          </g>

          <!-- ретрай прямо в узле: таблицы тасков на странице рана нет -->
          <g
            v-if="retryable(n)"
            role="button"
            tabindex="0"
            :aria-label="`Ретрай таска ${n.name}`"
            class="cursor-pointer"
            :transform="`translate(${NODE_W - 30}, 8)`"
            @click.stop="onRetryClick(n)"
            @keydown.enter.stop.prevent="onRetryClick(n)"
          >
            <title>Ретрай таска {{ n.name }}</title>
            <rect width="22" height="22" rx="6" fill="var(--ui-bg-elevated)" stroke="var(--ui-border-accented)" />
            <!-- lucide rotate-ccw, 24×24 → 14×14 -->
            <g transform="translate(4, 4) scale(0.583)" fill="none" stroke="var(--ui-text-toned)" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" />
            </g>
          </g>
        </g>
      </svg>
    </div>

    <div class="mt-1.5 flex items-center gap-4 text-xs text-muted">
      <span class="flex items-center gap-1.5">
        <svg width="28" height="8" class="shrink-0"><line x1="0" y1="4" x2="28" y2="4" stroke="var(--ui-border-accented)" stroke-width="1.5" /></svg>
        обычное ребро (ждёт успеха)
      </span>
      <span class="flex items-center gap-1.5">
        <svg width="28" height="8" class="shrink-0"><line x1="0" y1="4" x2="28" y2="4" stroke="var(--ui-border-accented)" stroke-width="1.5" stroke-dasharray="6 4" /></svg>
        стримовое (ко-старт, чтение по мере записи)
      </span>
      <span v-if="retryTasks?.length" class="flex items-center gap-1.5">
        <UIcon name="i-lucide-rotate-ccw" class="size-3.5 shrink-0" />
        ретрай таска (сбрасывает его downstream-подграф)
      </span>
    </div>
  </div>
</template>
