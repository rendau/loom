<script setup lang="ts">
import { setDagSchedule } from '~/api/dag.api'
import type { Dag } from '~/types/dag'

const props = defineProps<{
  dag: Dag | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const schedule = ref('')
const catchup = ref(false)
const action = useApiAction()

watch(() => props.dag, (dag) => {
  schedule.value = dag?.schedule ?? ''
  catchup.value = dag?.catchup ?? false
})

async function submit() {
  const dag = props.dag
  if (!dag)
    return
  const value = schedule.value.trim()
  const ok = await action.run(
    () => setDagSchedule(dag.name, value, catchup.value),
    { success: value ? 'Расписание сохранено' : 'Расписание снято' },
  )
  if (ok !== undefined)
    emit('saved')
}
</script>

<template>
  <UModal
    :open="dag !== null"
    title="Расписание"
    :description="`Даг ${dag?.name ?? ''}. Пустое расписание — запуск только вручную.`"
    @update:open="emit('close')"
  >
    <template #body>
      <div class="space-y-4">
        <UFormField label="Cron-выражение" hint="5 полей или @hourly / @daily / @weekly">
          <UInput
            v-model="schedule"
            class="w-full font-mono"
            placeholder="0 3 * * *"
            autofocus
            @keyup.enter="submit"
          />
        </UFormField>
        <UCheckbox
          v-model="catchup"
          label="Catchup"
          description="Наверстывать пропущенные тики расписания (ран на каждый тик, logical_date = тик). Без него пропущенное теряется, расписание продолжается от «сейчас»."
        />
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="Отмена" @click="emit('close')" />
        <UButton label="Сохранить" :loading="action.loading.value" @click="submit" />
      </div>
    </template>
  </UModal>
</template>
