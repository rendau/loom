<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import type { DagTask, TaskResourcesOverride } from '~/types/dag'

// Таски по манифесту: одна и та же таблица у дага-инстанса (таба «Схема»)
// и у шаблона в образе — устройство задаёт код, различаются только
// оверрайды ресурсов, которые есть лишь у инстанса.

const props = withDefaults(defineProps<{
  tasks: DagTask[]
  // оверрайды админки по имени таска; пусто — показываем значения кода
  overrides?: Map<string, TaskResourcesOverride>
  canManage?: boolean
}>(), {
  overrides: () => new Map(),
  canManage: false,
})

const emit = defineEmits<{ editResources: [task: string] }>()

function formatSeconds(sec: number): string {
  if (!sec)
    return '—'
  if (sec % 3600 === 0)
    return `${sec / 3600}ч`
  if (sec % 60 === 0)
    return `${sec / 60}м`
  return `${sec}с`
}

// эффективные ресурсы: непустое поле оверрайда из админки перекрывает
// значение манифеста
function effectiveResources(t: DagTask) {
  const r = t.resources
  const o = props.overrides.get(t.name)
  if (!o)
    return r
  return {
    cpu_request: o.cpu_request || r?.cpu_request || '',
    cpu_limit: o.cpu_limit || r?.cpu_limit || '',
    memory_request: o.memory_request || r?.memory_request || '',
    memory_limit: o.memory_limit || r?.memory_limit || '',
  }
}

function formatResources(t: DagTask): string {
  const r = effectiveResources(t)
  if (!r)
    return '—'
  const parts: string[] = []
  if (r.cpu_request || r.cpu_limit)
    parts.push(`cpu ${r.cpu_request || '—'}/${r.cpu_limit || '—'}`)
  if (r.memory_request || r.memory_limit)
    parts.push(`mem ${r.memory_request || '—'}/${r.memory_limit || '—'}`)
  return parts.length ? parts.join(' · ') : '—'
}

const columns: TableColumn<DagTask>[] = [
  { accessorKey: 'name', header: 'Таск' },
  { id: 'depends_on', header: 'Зависимости' },
  { accessorKey: 'retries', header: 'Ретраи' },
  { id: 'timeout', header: 'Таймаут' },
  { id: 'resources', header: 'Ресурсы (req/lim)' },
  { accessorKey: 'priority', header: 'Приоритет' },
  { id: 'injections', header: 'Инъекции' },
]
</script>

<template>
  <UTable :data="tasks" :columns="columns" :ui="denseTableUi">
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
      <div class="flex items-center gap-1.5">
        <span class="font-mono text-xs">{{ formatResources(row.original) }}</span>
        <UTooltip v-if="overrides.has(row.original.name)" text="лимиты заданы в админке; значения из кода — рекомендуемые">
          <UBadge color="info" variant="subtle" size="sm">админка</UBadge>
        </UTooltip>
        <UTooltip v-if="canManage" text="Изменить лимиты таска">
          <UButton
            icon="i-lucide-pencil" size="xs" color="neutral" variant="ghost"
            aria-label="Изменить лимиты таска" @click="emit('editResources', row.original.name)"
          />
        </UTooltip>
      </div>
    </template>
    <template #priority-cell="{ row }">
      {{ row.original.priority || '—' }}
    </template>
    <template #injections-cell="{ row }">
      <div v-if="row.original.secrets?.length || row.original.variables?.length" class="flex flex-wrap gap-1">
        <UTooltip v-for="s in row.original.secrets" :key="`s-${s.env}`" :text="`env ${s.env}`">
          <UBadge color="warning" variant="subtle" size="sm" class="font-mono">
            <UIcon name="i-lucide-key-round" class="size-3" />
            {{ s.secret }}
          </UBadge>
        </UTooltip>
        <UTooltip v-for="v in row.original.variables" :key="`v-${v.env}`" :text="`env ${v.env}`">
          <UBadge color="neutral" variant="subtle" size="sm" class="font-mono">
            <UIcon name="i-lucide-variable" class="size-3" />
            {{ v.variable }}
          </UBadge>
        </UTooltip>
      </div>
      <span v-else class="text-muted">—</span>
    </template>
  </UTable>
</template>
