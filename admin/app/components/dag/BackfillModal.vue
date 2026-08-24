<script setup lang="ts">
import { backfillRuns } from '~/api/run.api'
import type { Dag } from '~/types/dag'

// Backfill: ран на каждый тик расписания дага в периоде [from, to).

const props = defineProps<{
  dag: Dag | null
}>()

const emit = defineEmits<{ close: [] }>()

const from = ref('')
const to = ref('')
const params = ref('')
const action = useApiAction()
const toast = useToast()

watch(() => props.dag, () => {
  from.value = ''
  to.value = ''
  params.value = ''
})

async function confirm() {
  const dag = props.dag
  if (!dag)
    return
  if (!from.value || !to.value) {
    toast.add({ title: 'Задайте период from и to', color: 'error' })
    return
  }
  const parsed = parseRunParams(params.value)
  if (parsed === null) {
    toast.add({ title: 'Параметры должны быть JSON-объектом', color: 'error' })
    return
  }

  const rep = await action.run(() => backfillRuns(
    dag.name,
    new Date(from.value).toISOString(),
    new Date(to.value).toISOString(),
    parsed,
  ))
  if (rep) {
    toast.add({ title: `Создано ранов: ${rep.run_ids.length}`, color: 'success' })
    emit('close')
    await navigateTo('/runs')
  }
}
</script>

<template>
  <UModal
    :open="dag !== null"
    title="Backfill"
    :description="`Ран на каждый тик расписания «${dag?.schedule ?? ''}» в периоде [from, to).`"
    @update:open="emit('close')"
  >
    <template #body>
      <div class="space-y-4">
        <div class="grid grid-cols-2 gap-3">
          <UFormField label="From (включительно)">
            <UInput v-model="from" type="datetime-local" class="w-full" />
          </UFormField>
          <UFormField label="To (исключительно)">
            <UInput v-model="to" type="datetime-local" class="w-full" />
          </UFormField>
        </div>
        <UFormField label="Параметры всех ранов (JSON-объект, опционально)">
          <UTextarea
            v-model="params"
            class="w-full font-mono"
            :rows="3"
            placeholder='{"source": "backfill"}'
          />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="Отмена" @click="emit('close')" />
        <UButton label="Создать раны" :loading="action.loading.value" @click="confirm" />
      </div>
    </template>
  </UModal>
</template>
