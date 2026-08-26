<script setup lang="ts">
import { deleteDag } from '~/api/dag.api'
import type { Dag } from '~/types/dag'

const props = defineProps<{
  dag: Dag | null
}>()

const emit = defineEmits<{
  close: []
  deleted: []
}>()

const action = useApiAction()

async function confirm() {
  const dag = props.dag
  if (!dag)
    return
  const ok = await action.run(() => deleteDag(dag), { success: 'Даг удалён' })
  if (ok !== undefined)
    emit('deleted')
}
</script>

<template>
  <UModal :open="dag !== null" title="Удалить даг?" @update:open="emit('close')">
    <template #body>
      <p>
        Даг <span class="font-mono font-medium">{{ dag ? dagRefLabel(dag) : '' }}</span> будет удалён.
        История его ранов останется.
      </p>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="Отмена" @click="emit('close')" />
        <UButton color="error" label="Удалить" :loading="action.loading.value" @click="confirm" />
      </div>
    </template>
  </UModal>
</template>
