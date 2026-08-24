<script setup lang="ts">
// Слайдовер лога попытки — тонкая обёртка над RunLogViewer (вся логика
// стрима/фильтров там). Кнопка «на весь экран» ведёт на отдельную страницу.

const props = defineProps<{ runId: string }>()

const open = ref(false)
const task = ref('')
const attempt = ref(0)
const follow = ref(false)

function show(taskName: string, attemptNumber: number, followMode: boolean) {
  task.value = taskName
  attempt.value = attemptNumber
  follow.value = followMode
  open.value = true
}

const fullscreenTo = computed(() =>
  `/runs/${encodeURIComponent(props.runId)}/log?task=${encodeURIComponent(task.value)}`
  + `&attempt=${attempt.value}&follow=${follow.value ? '1' : '0'}`)

defineExpose({ show })
</script>

<template>
  <USlideover
    v-model:open="open"
    :title="`Лог: ${task} · попытка ${attempt}`"
    :ui="{ content: 'max-w-5xl', body: 'p-0 sm:p-0 overflow-hidden flex flex-col' }"
  >
    <template #description>
      <UButton
        :to="fullscreenTo"
        icon="i-lucide-maximize-2"
        label="На весь экран"
        color="neutral"
        variant="link"
        size="xs"
        class="p-0"
      />
    </template>

    <template #body>
      <RunLogViewer
        v-if="open"
        :run-id="runId"
        :task="task"
        :attempt="attempt"
        :follow="follow"
        class="flex-1"
      />
    </template>
  </USlideover>
</template>
