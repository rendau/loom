<script setup lang="ts">
import { triggerRun } from '~/api/run.api'
import type { Dag } from '~/types/dag'

// Ручной запуск рана с опциональными params (JSON-объект).

const props = defineProps<{
  dag: Dag | null
}>()

const emit = defineEmits<{ close: [] }>()

const params = ref('')
const action = useApiAction()
const toast = useToast()

watch(() => props.dag, () => {
  params.value = ''
})

async function confirm() {
  const dag = props.dag
  if (!dag)
    return
  const parsed = parseRunParams(params.value)
  if (parsed === null) {
    toast.add({ title: 'Параметры должны быть JSON-объектом', color: 'error' })
    return
  }
  const rep = await action.run(() => triggerRun(dag, parsed), { success: 'Ран запущен' })
  if (rep) {
    emit('close')
    await navigateTo(`/runs/${rep.run_id}`)
  }
}
</script>

<template>
  <UModal
    :open="dag !== null"
    title="Запуск рана"
    :description="`Даг ${dag?.name ?? ''}. Параметры доступны таскам через rt.Params().`"
    @update:open="emit('close')"
  >
    <template #body>
      <UFormField label="Параметры (JSON-объект, опционально)">
        <UTextarea
          v-model="params"
          class="w-full font-mono"
          :rows="4"
          placeholder='{"date": "2026-08-01"}'
        />
      </UFormField>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="Отмена" @click="emit('close')" />
        <UButton label="Запустить" :loading="action.loading.value" @click="confirm" />
      </div>
    </template>
  </UModal>
</template>
