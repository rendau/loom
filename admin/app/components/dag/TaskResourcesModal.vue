<script setup lang="ts">
import type { DagRef } from '~/types/common'
import { setTaskResources } from '~/api/dag.api'
import type { DagTask, TaskResourcesOverride } from '~/types/dag'

// Редактор оверрайда ресурсов таска: значения из кода дага (манифеста) —
// рекомендуемые (плейсхолдеры), непустое поле — приоритет админки.
// Все поля пустые — оверрайд снимается.

const props = defineProps<{
  dagRef: DagRef
  task: DagTask | null
  override: TaskResourcesOverride | null
}>()

const emit = defineEmits<{ close: [], saved: [] }>()

const action = useApiAction()

const fields = [
  { key: 'cpu_request', label: 'CPU request', hint: 'напр. 100m' },
  { key: 'cpu_limit', label: 'CPU limit', hint: 'напр. 500m' },
  { key: 'memory_request', label: 'Memory request', hint: 'напр. 128Mi' },
  { key: 'memory_limit', label: 'Memory limit', hint: 'напр. 512Mi' },
] as const

type FieldKey = typeof fields[number]['key']

const values = ref<Record<FieldKey, string>>({
  cpu_request: '',
  cpu_limit: '',
  memory_request: '',
  memory_limit: '',
})

watch(() => props.task, (task) => {
  if (!task)
    return
  for (const f of fields)
    values.value[f.key] = props.override?.[f.key] ?? ''
}, { immediate: true })

function manifestValue(key: FieldKey): string {
  return props.task?.resources?.[key] ?? ''
}

// kubernetes quantity: число с опциональным суффиксом (Ki/Mi/Gi/…/m/k/M/G)
const quantityRe = /^\d+(?:\.\d+)?(?:[numkKMGTPE]i?)?$/

function fieldValid(key: FieldKey): boolean {
  const v = values.value[key].trim()
  return v === '' || quantityRe.test(v)
}

const allValid = computed(() => fields.every(f => fieldValid(f.key)))
const isEmpty = computed(() => fields.every(f => values.value[f.key].trim() === ''))

async function save() {
  const task = props.task
  if (!task)
    return
  const ok = await action.run(() => setTaskResources(props.dagRef, task.name, {
    cpu_request: values.value.cpu_request.trim(),
    cpu_limit: values.value.cpu_limit.trim(),
    memory_request: values.value.memory_request.trim(),
    memory_limit: values.value.memory_limit.trim(),
  }), { success: isEmpty.value ? 'Оверрайд снят — действуют значения из кода' : 'Лимиты таска сохранены' })
  if (ok !== undefined) {
    emit('saved')
    emit('close')
  }
}
</script>

<template>
  <UModal
    :open="task !== null"
    :title="`Ресурсы таска ${task?.name ?? ''}`"
    description="Значения из кода дага — рекомендуемые; заполненное здесь поле приоритетнее и применяется со следующего запуска попытки (перерегистрация не нужна). Пустое поле — из кода."
    @update:open="emit('close')"
  >
    <template #body>
      <div class="grid grid-cols-2 gap-4">
        <UFormField
          v-for="f in fields"
          :key="f.key"
          :label="f.label"
          :help="manifestValue(f.key) ? `в коде: ${manifestValue(f.key)}` : 'в коде не задано'"
          :ui="{ label: 'whitespace-nowrap' }"
        >
          <UInput
            v-model="values[f.key]"
            class="w-full font-mono"
            :color="fieldValid(f.key) ? undefined : 'error'"
            :placeholder="manifestValue(f.key) || f.hint"
          />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full items-center justify-between gap-2">
        <p v-if="isEmpty && override" class="text-xs text-muted">
          Все поля пустые — оверрайд будет снят.
        </p>
        <span v-else />
        <div class="flex gap-2">
          <UButton color="neutral" variant="ghost" label="Отмена" @click="emit('close')" />
          <UButton label="Сохранить" :disabled="!allValid" :loading="action.loading.value" @click="save" />
        </div>
      </div>
    </template>
  </UModal>
</template>
