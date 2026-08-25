<script setup lang="ts">
import type { DagRegistration } from '~/types/dag'

// Панель регистраций над списком дагов. Регистрируют обычно пачкой образов,
// поэтому по алерту на образ нельзя: список дагов уезжает за экран. Пачка
// сворачивается в одну строку-сводку с раскрывающимся (и скроллящимся)
// списком; одиночная регистрация показывается целиком — раскрывать нечего.
const props = defineProps<{
  active: DagRegistration[]
  failed: DagRegistration[]
}>()

// провалы висят сутки — прочитанную причину надо уметь убрать с глаз
// (активные закрывать нечем: они уходят сами, когда доедут)
const emit = defineEmits<{ dismissFailed: [] }>()

const runningCount = computed(() => props.active.filter(r => r.status === 'running').length)

const activeTitle = computed(() =>
  props.active.length === 1
    ? `Регистрация ${props.active[0]!.image}`
    : `Регистрируются образы: ${props.active.length}`)

const activeDescription = computed(() => {
  if (props.active.length === 1)
    return props.active[0]!.status === 'pending' ? 'В очереди…' : 'Выполняется pull + describe…'
  const queued = props.active.length - runningCount.value
  return `Выполняется: ${runningCount.value} · в очереди: ${queued}`
})

const failedTitle = computed(() =>
  props.failed.length === 1
    ? `Регистрация ${props.failed[0]!.dag_name || props.failed[0]!.image} не удалась (${formatDateTime(props.failed[0]!.finished_at)})`
    : `Регистрации не удались: ${props.failed.length} (за последние сутки)`)
</script>

<template>
  <div v-if="active.length > 0 || failed.length > 0" class="space-y-2">
    <UAlert
      v-if="active.length > 0"
      color="info"
      variant="subtle"
      icon="i-lucide-loader-circle"
      :ui="{ icon: 'animate-spin' }"
      :title="activeTitle"
    >
      <template #description>
        <p>{{ activeDescription }}</p>
        <UCollapsible v-if="active.length > 1">
          <UButton
            :label="`Показать образы (${active.length})`"
            color="info"
            variant="link"
            size="xs"
            trailing-icon="i-lucide-chevron-down"
            class="group p-0"
            :ui="{ trailingIcon: 'transition-transform duration-200 group-data-[state=open]:rotate-180' }"
          />
          <template #content>
            <ul class="mt-1 max-h-40 space-y-1 overflow-y-auto pr-1 font-mono text-xs">
              <li v-for="reg in active" :key="reg.id" class="flex items-center gap-2">
                <UIcon
                  :name="reg.status === 'running' ? 'i-lucide-loader-circle' : 'i-lucide-clock'"
                  class="size-3 shrink-0"
                  :class="reg.status === 'running' && 'animate-spin'"
                />
                <span class="truncate">{{ reg.image }}</span>
              </li>
            </ul>
          </template>
        </UCollapsible>
      </template>
    </UAlert>

    <UAlert
      v-if="failed.length > 0"
      color="error"
      variant="subtle"
      icon="i-lucide-circle-x"
      close
      :title="failedTitle"
      @update:open="emit('dismissFailed')"
    >
      <template #description>
        <p v-if="failed.length === 1">{{ failed[0]!.error }}</p>
        <UCollapsible v-else>
          <UButton
            :label="`Показать причины (${failed.length})`"
            color="error"
            variant="link"
            size="xs"
            trailing-icon="i-lucide-chevron-down"
            class="group p-0"
            :ui="{ trailingIcon: 'transition-transform duration-200 group-data-[state=open]:rotate-180' }"
          />
          <template #content>
            <ul class="mt-1 max-h-48 space-y-1.5 overflow-y-auto pr-1 text-xs">
              <li v-for="reg in failed" :key="reg.id">
                <div class="font-mono">{{ reg.dag_name || reg.image }}</div>
                <div class="text-dimmed">
                  {{ formatDateTime(reg.finished_at) }} — {{ reg.error }}
                </div>
              </li>
            </ul>
          </template>
        </UCollapsible>
      </template>
    </UAlert>
  </div>
</template>
