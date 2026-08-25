<script setup lang="ts">
// Относительное время для списков («5 мин назад», «вчера 14:03»);
// абсолютное — в tooltip (design/05: в списках относительное, абсолют в
// деталях). Общий минутный тик, чтобы значения не застывали.

const props = defineProps<{
  time?: string
  // переопределение tooltip'а (например «Обновлено … · Создано …»)
  tooltip?: string
}>()

const now = useTimeTick()
const text = computed(() => formatRelative(props.time, now.value))
</script>

<template>
  <UTooltip v-if="time" :text="tooltip ?? formatDateTime(time)">
    <span class="whitespace-nowrap">{{ text }}</span>
  </UTooltip>
  <span v-else class="text-muted">—</span>
</template>
