<script setup lang="ts">
import { readTaskLog } from '~/api/log.api'
import type { TaskLogEntry } from '~/types/log'

// Просмотр лога попытки: содержимое подгружается стримом с gateway; в
// follow-режиме стрим живёт до завершения попытки (live-логи).

const props = defineProps<{ runId: string }>()

const open = ref(false)
const task = ref('')
const attempt = ref(0)
const entries = ref<TaskLogEntry[]>([])
const errorText = ref('')
const streaming = ref(false)
const following = ref(false)

const bodyRef = ref<HTMLElement | null>(null)
let ctrl: AbortController | undefined

async function show(taskName: string, attemptNumber: number, follow: boolean) {
  task.value = taskName
  attempt.value = attemptNumber
  following.value = follow
  open.value = true
  await start()
}

async function start() {
  ctrl?.abort()
  const myCtrl = new AbortController()
  ctrl = myCtrl

  entries.value = []
  errorText.value = ''
  streaming.value = true

  try {
    await readTaskLog(
      { runId: props.runId, task: task.value, attempt: attempt.value, follow: following.value },
      (batch) => {
        entries.value.push(...batch)
        scrollDown()
      },
      myCtrl.signal,
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

function scrollDown() {
  nextTick(() => {
    const el = bodyRef.value
    if (el)
      el.scrollTop = el.scrollHeight
  })
}

watch(open, (value) => {
  if (!value)
    ctrl?.abort()
})

onBeforeUnmount(() => ctrl?.abort())

defineExpose({ show })
</script>

<template>
  <USlideover
    v-model:open="open"
    :title="`Лог: ${task} · попытка ${attempt}`"
    :ui="{ content: 'max-w-3xl' }"
  >
    <template #description>
      <span class="flex items-center gap-2">
        <UBadge v-if="streaming" color="info" variant="subtle" size="sm">
          <UIcon name="i-lucide-radio" class="mr-1 size-3 animate-pulse" />
          live
        </UBadge>
        <USwitch v-model="following" label="Follow" size="sm" @update:model-value="start" />
      </span>
    </template>

    <template #body>
      <div ref="bodyRef" class="h-full overflow-y-auto rounded-md bg-elevated p-3 font-mono text-xs leading-5">
        <UAlert v-if="errorText" color="error" variant="subtle" :title="errorText" class="mb-2 font-sans" />

        <div v-for="(entry, i) in entries" :key="i" class="flex gap-2 whitespace-pre-wrap break-all">
          <span class="shrink-0 text-dimmed">{{ formatTimestampMs(entry.ts_unix_ms) }}</span>
          <span class="shrink-0 w-7" :class="logSourceClass(entry.source)">{{ logSourceLabel(entry.source) }}</span>
          <span class="text-default">{{ entry.line }}</span>
        </div>

        <div v-if="!streaming && !errorText && entries.length === 0" class="text-muted">
          Лог пуст.
        </div>
      </div>
    </template>
  </USlideover>
</template>
