<script setup lang="ts">
// Полноэкранный просмотр лога попытки (?task=...&attempt=N&follow=0|1).

const route = useRoute()
const runId = String(route.params.id)
const task = String(route.query.task ?? '')
const attempt = Number(route.query.attempt ?? 1)
const follow = route.query.follow === '1'
</script>

<template>
  <UDashboardPanel id="run-log" :ui="{ body: 'p-0 sm:p-0 gap-0 overflow-hidden' }">
    <template #header>
      <UDashboardNavbar :title="`Лог: ${task} · попытка ${attempt}`">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" :to="`/runs/${encodeURIComponent(runId)}`" />
        </template>
        <template #right>
          <span class="font-mono text-xs text-muted">{{ runId }}</span>
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <RunLogViewer :run-id="runId" :task="task" :attempt="attempt" :follow="follow" class="h-full" />
    </template>
  </UDashboardPanel>
</template>
