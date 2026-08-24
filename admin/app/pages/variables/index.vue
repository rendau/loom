<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import { deleteVariable, listVariables, setVariable } from '~/api/variable.api'
import type { Variable } from '~/types/variable'

// Переменные для env-инъекции в поды тасков: как секреты, но значения
// видны в списке. Скоупы: глобальный ('') и локальный для дага; локальный
// перекрывает глобальный при запуске.

const route = useRoute()
const { canManageDag } = useAuth()

// право менять запись: глобальные — только admin, локальные — владелец дага
function canManageScope(dagName: string): boolean {
  return canManageDag(dagName || undefined)
}

const variables = ref<Variable[]>([])
const loading = ref(false)
const action = useApiAction()
const toast = useToast()

// фильтр скоупа: undefined — все, '' — глобальные, имя дага — его локальные
const ALL_SCOPES = '__all__'
// reka-ui Select не принимает '' как value опции — глобальный скоуп через sentinel
const GLOBAL_SCOPE = '__global__'
const scopeFilter = ref<string>(
  typeof route.query.dag_name === 'string' ? (route.query.dag_name || GLOBAL_SCOPE) : ALL_SCOPES,
)
const dagNames = ref<string[]>([])

const scopeItems = computed(() => [
  { label: 'Все скоупы', value: ALL_SCOPES },
  { label: 'Глобальные', value: GLOBAL_SCOPE },
  ...dagNames.value.map(n => ({ label: `Даг: ${n}`, value: n })),
])

async function load() {
  loading.value = true
  try {
    const dagName = scopeFilter.value === ALL_SCOPES
      ? undefined
      : scopeFilter.value === GLOBAL_SCOPE ? '' : scopeFilter.value
    const rep = await listVariables(dagName)
    variables.value = rep.results ?? []
  }
  catch (error) {
    toast.add({ title: 'Ошибка загрузки переменных', description: apiErrorMessage(error), color: 'error' })
  }
  finally {
    loading.value = false
  }
}

async function loadDagNames() {
  try {
    const rep = await listDags({ list_params: { page_size: 500, sort: ['name'] } })
    dagNames.value = rep.results.map(d => d.name)
  }
  catch {
    // фильтр по дагам просто останется без вариантов
  }
}

onMounted(async () => {
  await Promise.all([load(), loadDagNames()])
})
watch(scopeFilter, load)

// создание / изменение
const editOpen = ref(false)
const editScope = ref('')
const editName = ref('')
const editValue = ref('')
const editIsNew = ref(true)

const editScopeItems = computed(() => [
  { label: 'Глобальный', value: GLOBAL_SCOPE },
  ...dagNames.value.map(n => ({ label: `Даг: ${n}`, value: n })),
])

function openCreate() {
  editIsNew.value = true
  editScope.value = scopeFilter.value === ALL_SCOPES ? GLOBAL_SCOPE : scopeFilter.value
  editName.value = ''
  editValue.value = ''
  editOpen.value = true
}

function openEdit(v: Variable) {
  editIsNew.value = false
  editScope.value = v.dag_name || GLOBAL_SCOPE
  editName.value = v.name
  editValue.value = v.value
  editOpen.value = true
}

async function submitEdit() {
  const name = editName.value.trim()
  if (!name)
    return
  const dagName = editScope.value === GLOBAL_SCOPE ? '' : editScope.value
  const ok = await action.run(() => setVariable(dagName, name, editValue.value), { success: 'Переменная сохранена' })
  if (ok !== undefined) {
    editOpen.value = false
    await load()
  }
}

const deleteTarget = ref<Variable | null>(null)

async function confirmDelete() {
  const variable = deleteTarget.value
  if (!variable)
    return
  const ok = await action.run(() => deleteVariable(variable.dag_name, variable.name), { success: 'Переменная удалена' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<Variable>[] = [
  { accessorKey: 'name', header: 'Переменная' },
  { accessorKey: 'dag_name', header: 'Скоуп' },
  { accessorKey: 'value', header: 'Значение' },
  { accessorKey: 'created_at', header: 'Создана' },
  { accessorKey: 'modified_at', header: 'Изменена' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="variables">
    <template #header>
      <UDashboardNavbar title="Переменные">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
          <UButton icon="i-lucide-plus" label="Создать" @click="openCreate" />
        </template>
      </UDashboardNavbar>
      <UDashboardToolbar>
        <template #left>
          <USelect v-model="scopeFilter" :items="scopeItems" value-key="value" class="w-64" />
        </template>
      </UDashboardToolbar>
    </template>

    <template #body>
      <UTable :data="variables" :columns="columns" :loading="loading" :ui="{ td: 'whitespace-normal' }">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-variable" class="size-4 shrink-0 text-muted" />
            <span class="font-mono font-medium text-highlighted">{{ row.original.name }}</span>
          </div>
        </template>

        <template #dag_name-cell="{ row }">
          <UBadge v-if="row.original.dag_name" color="info" variant="subtle" size="sm">
            {{ row.original.dag_name }}
          </UBadge>
          <UBadge v-else color="neutral" variant="subtle" size="sm">глобальный</UBadge>
        </template>

        <template #value-cell="{ row }">
          <div class="max-w-md break-all font-mono text-xs" :title="row.original.value">
            {{ row.original.value.length > 120 ? `${row.original.value.slice(0, 119)}…` : row.original.value }}
          </div>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #modified_at-cell="{ row }">
          {{ formatDateTime(row.original.modified_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip v-if="canManageScope(row.original.dag_name)" text="Изменить значение">
              <UButton
                icon="i-lucide-pencil"
                size="sm"
                color="neutral"
                variant="ghost"
                @click="openEdit(row.original)"
              />
            </UTooltip>
            <UTooltip v-if="canManageScope(row.original.dag_name)" text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <div v-if="!loading && variables.length === 0" class="p-8 text-center text-muted">
        Переменных нет. Таск подключает переменную опцией loom.Variable(envName, varName).
      </div>

      <!-- создание / изменение -->
      <UModal
        v-model:open="editOpen"
        :title="editIsNew ? 'Создание переменной' : `Переменная ${editName}`"
      >
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="editIsNew" label="Скоуп" hint="локальный перекрывает глобальный с тем же именем">
              <USelect v-model="editScope" :items="editScopeItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField v-if="editIsNew" label="Имя переменной">
              <UInput v-model="editName" class="w-full" placeholder="api-base-url" autofocus />
            </UFormField>
            <UFormField label="Значение">
              <UTextarea v-model="editValue" class="w-full font-mono" :rows="3" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
            <UButton label="Сохранить" :loading="action.loading.value" @click="submitEdit" />
          </div>
        </template>
      </UModal>

      <!-- подтверждение удаления -->
      <UModal :open="deleteTarget !== null" title="Удалить переменную?" @update:open="deleteTarget = null">
        <template #body>
          <p>
            Переменная <span class="font-mono font-medium">{{ deleteTarget?.name }}</span>
            <template v-if="deleteTarget?.dag_name">
              (даг <span class="font-mono">{{ deleteTarget?.dag_name }}</span>)
            </template>
            будет удалена. Таски, ссылающиеся на неё, перестанут запускаться (launch_failed).
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="deleteTarget = null" />
            <UButton color="error" label="Удалить" :loading="action.loading.value" @click="confirmDelete" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
