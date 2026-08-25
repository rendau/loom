<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import type { RunEnvBinding } from '~/utils/runenv'

// Окружение рана/таска: переменная/секрет — под тем именем, под которым
// заведена в админке (env-имя контейнера — деталь кода дага, в тултипе).
// snapshot=true — данные из run_env (фактическая инъекция при launch);
// false — fallback для старых ранов: текущие значения с обязательной
// пометкой. Секрет — только имя, «••••» и переход в /env (показ под RBAC).

const props = defineProps<{
  bindings: RunEnvBinding[]
  snapshot?: boolean
}>()

// тип — иконкой слева от имени: отдельная колонка ради двух слов съедала
// место, которое нужнее значению
const columns: TableColumn<RunEnvBinding>[] = [
  { id: 'source', header: 'Имя' },
  { id: 'value', header: 'Значение' },
  { id: 'scope', header: 'Скоуп' },
]

const hasVariables = computed(() => props.bindings.some(b => b.kind === 'variable'))

function envLink(b: RunEnvBinding): string {
  const query = new URLSearchParams({ kind: b.kind, q: b.name })
  if (b.scope)
    query.set('dag_name', b.scope)
  return `/env?${query.toString()}`
}
</script>

<template>
  <div>
    <UTable :data="bindings" :columns="columns" :ui="denseTableUi">
      <template #source-cell="{ row }">
        <div class="flex items-center gap-1.5">
          <UTooltip
            :text="`${row.original.kind === 'secret' ? 'секрет' : 'переменная'} · в контейнере: ${row.original.env}`"
          >
            <UIcon
              :name="row.original.kind === 'secret' ? 'i-lucide-key-round' : 'i-lucide-variable'"
              class="size-3.5 shrink-0"
              :class="row.original.kind === 'secret' ? 'text-warning' : 'text-dimmed'"
            />
          </UTooltip>
          <UTooltip :text="`в контейнере: ${row.original.env}`">
            <NuxtLink :to="envLink(row.original)" class="font-mono text-xs font-medium text-highlighted hover:text-primary hover:underline">
              {{ row.original.name }}
            </NuxtLink>
          </UTooltip>
        </div>
      </template>

      <template #value-cell="{ row }">
        <template v-if="row.original.kind === 'variable' && row.original.value !== undefined">
          <span class="block max-w-80 truncate font-mono text-xs font-medium text-highlighted" :title="row.original.value">
            {{ row.original.value || '—' }}
          </span>
        </template>
        <span v-else-if="row.original.kind === 'secret' && row.original.scope !== undefined" class="font-mono text-xs text-muted">••••••</span>
        <span v-else class="text-muted">—</span>
      </template>

      <!-- скоуп — последней колонкой: имя и значение должны стоять рядом -->
      <template #scope-cell="{ row }">
        <UBadge v-if="row.original.scope === undefined" color="error" variant="subtle" size="sm">
          не найдена — запуск таска упадёт
        </UBadge>
        <UBadge v-else-if="row.original.scope === ''" color="neutral" variant="subtle" size="sm">глобальный</UBadge>
        <UBadge v-else color="info" variant="subtle" size="sm">{{ row.original.scope }}</UBadge>
      </template>

      <template #empty>
        <p class="py-4 text-center text-sm text-muted">Env-привязок нет.</p>
      </template>
    </UTable>

    <p v-if="hasVariables" class="mt-1.5 flex items-center gap-1 text-xs text-muted">
      <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
      <template v-if="snapshot">
        Снапшот на момент запуска — то, что реально ушло в поды тасков.
      </template>
      <template v-else>
        Показаны текущие значения переменных — они могли измениться после запуска рана.
      </template>
    </p>
  </div>
</template>
