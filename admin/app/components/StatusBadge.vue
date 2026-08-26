<script setup lang="ts">
import type { AttemptStatus, RunStatus, TaskStatus } from '~/types/run'
import type { ProjectRegistrationStatus } from '~/types/project'

// Единый рендер статуса: иконка + подпись + цвет (design/06 §3). Цвет
// никогда не остаётся единственным сигналом. Словарь — utils/status.ts.

const props = defineProps<{
  kind: 'run' | 'task' | 'attempt' | 'registration'
  status: string
  size?: 'sm' | 'md' | 'lg'
}>()

const meta = computed(() => {
  switch (props.kind) {
    case 'run': {
      const s = props.status as RunStatus
      return { color: runStatusColor(s), label: runStatusLabel(s), icon: runStatusIcon(s) }
    }
    case 'task': {
      const s = props.status as TaskStatus
      return { color: taskStatusColor(s), label: taskStatusLabel(s), icon: taskStatusIcon(s) }
    }
    case 'attempt': {
      const s = props.status as AttemptStatus
      return { color: attemptStatusColor(s), label: s, icon: attemptStatusIcon(s) }
    }
    case 'registration': {
      const s = props.status as ProjectRegistrationStatus
      return { color: regStatusColor(s), label: regStatusLabel(s), icon: regStatusIcon(s) }
    }
    default:
      return { color: 'neutral' as const, label: props.status, icon: 'i-lucide-circle' }
  }
})

const spin = computed(() => meta.value.icon === 'i-lucide-loader-circle')

// приглушаем состояния-следствия, чтобы не спорили с реальной проблемой
const dimmed = computed(() =>
  props.kind === 'task' && (props.status === 'upstream_failed' || props.status === 'canceled' || props.status === 'pending'))
</script>

<template>
  <UBadge
    :color="meta.color"
    variant="subtle"
    :size="size ?? 'md'"
    :class="dimmed ? 'opacity-70' : ''"
    class="whitespace-nowrap"
  >
    <UIcon :name="meta.icon" class="size-3.5 shrink-0" :class="spin ? 'animate-spin' : ''" />
    {{ meta.label }}<slot />
  </UBadge>
</template>
