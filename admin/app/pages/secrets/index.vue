<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import { deleteSecret, getSecretValue, listSecrets, setSecret } from '~/api/secret.api'
import type { SecretMeta } from '~/types/secret'

// Секреты для env-инъекции в поды тасков. Скоупы: глобальный ('') и
// локальный для дага; локальный перекрывает глобальный при запуске.
// Значение — по кнопке «показать» (getSecretValue, RBAC).

const route = useRoute()
const { canManageDag } = useAuth()

// право менять запись: глобальные — только admin, локальные — владелец дага
function canManageScope(dagName: string): boolean {
  return canManageDag(dagName || undefined)
}

const secrets = ref<SecretMeta[]>([])
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
    const rep = await listSecrets(dagName)
    secrets.value = rep.results ?? []
  }
  catch (error) {
    toast.add({ title: 'Ошибка загрузки секретов', description: apiErrorMessage(error), color: 'error' })
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

// создание / перезапись значения
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

function openRotate(secret: SecretMeta) {
  editIsNew.value = false
  editScope.value = secret.dag_name || GLOBAL_SCOPE
  editName.value = secret.name
  editValue.value = ''
  editOpen.value = true
}

async function submitEdit() {
  const name = editName.value.trim()
  if (!name || !editValue.value)
    return
  const dagName = editScope.value === GLOBAL_SCOPE ? '' : editScope.value
  const ok = await action.run(() => setSecret(dagName, name, editValue.value), { success: 'Секрет сохранён' })
  if (ok !== undefined) {
    editOpen.value = false
    editValue.value = ''
    await load()
  }
}

// просмотр значения по кнопке; ключ — скоуп/имя
const shownValues = ref<Record<string, string>>({})

function valueKey(s: SecretMeta): string {
  return `${s.dag_name} ${s.name}`
}

async function toggleValue(s: SecretMeta) {
  const key = valueKey(s)
  if (shownValues.value[key] !== undefined) {
    const { [key]: _, ...rest } = shownValues.value
    shownValues.value = rest
    return
  }
  const rep = await action.run(() => getSecretValue(s.dag_name, s.name), { silent: true })
  if (rep)
    shownValues.value = { ...shownValues.value, [key]: rep.value }
}

async function copyValue(s: SecretMeta) {
  const value = shownValues.value[valueKey(s)]
  if (value === undefined)
    return
  try {
    await navigator.clipboard.writeText(value)
    toast.add({ title: 'Значение скопировано', color: 'success' })
  }
  catch {
    // clipboard недоступен — молча
  }
}

const deleteTarget = ref<SecretMeta | null>(null)

async function confirmDelete() {
  const secret = deleteTarget.value
  if (!secret)
    return
  const ok = await action.run(() => deleteSecret(secret.dag_name, secret.name), { success: 'Секрет удалён' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<SecretMeta>[] = [
  { accessorKey: 'name', header: 'Секрет' },
  { accessorKey: 'dag_name', header: 'Скоуп' },
  { id: 'value', header: 'Значение' },
  { accessorKey: 'created_at', header: 'Создан' },
  { accessorKey: 'modified_at', header: 'Изменён' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="secrets">
    <template #header>
      <UDashboardNavbar title="Секреты">
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
      <UTable :data="secrets" :columns="columns" :loading="loading" :ui="{ td: 'whitespace-normal' }">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-key-round" class="size-4 shrink-0 text-muted" />
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
          <div class="flex items-center gap-1">
            <template v-if="shownValues[valueKey(row.original)] !== undefined">
              <span class="max-w-64 truncate font-mono text-xs" :title="shownValues[valueKey(row.original)]">
                {{ shownValues[valueKey(row.original)] }}
              </span>
              <UTooltip text="Скопировать значение">
                <UButton icon="i-lucide-copy" size="xs" color="neutral" variant="ghost" @click="copyValue(row.original)" />
              </UTooltip>
              <UTooltip text="Скрыть">
                <UButton icon="i-lucide-eye-off" size="xs" color="neutral" variant="ghost" @click="toggleValue(row.original)" />
              </UTooltip>
            </template>
            <template v-else>
              <span class="font-mono text-xs text-muted">••••••</span>
              <UTooltip text="Показать значение">
                <UButton icon="i-lucide-eye" size="xs" color="neutral" variant="ghost" @click="toggleValue(row.original)" />
              </UTooltip>
            </template>
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
            <UTooltip v-if="canManageScope(row.original.dag_name)" text="Заменить значение">
              <UButton
                icon="i-lucide-rotate-ccw-key"
                size="sm"
                color="neutral"
                variant="ghost"
                @click="openRotate(row.original)"
              />
            </UTooltip>
            <UTooltip v-if="canManageScope(row.original.dag_name)" text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <div v-if="!loading && secrets.length === 0" class="p-8 text-center text-muted">
        Секретов нет. Таск подключает секрет опцией loom.Secret(envName, secretName).
      </div>

      <!-- создание / перезапись -->
      <UModal
        v-model:open="editOpen"
        :title="editIsNew ? 'Создание секрета' : `Замена значения ${editName}`"
        description="Значение шифруется на сервере; посмотреть его можно только по кнопке в списке."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="editIsNew" label="Скоуп" hint="локальный перекрывает глобальный с тем же именем">
              <USelect v-model="editScope" :items="editScopeItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField v-if="editIsNew" label="Имя секрета">
              <UInput v-model="editName" class="w-full" placeholder="prod-db-password" autofocus />
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
      <UModal :open="deleteTarget !== null" title="Удалить секрет?" @update:open="deleteTarget = null">
        <template #body>
          <p>
            Секрет <span class="font-mono font-medium">{{ deleteTarget?.name }}</span>
            <template v-if="deleteTarget?.dag_name">
              (даг <span class="font-mono">{{ deleteTarget?.dag_name }}</span>)
            </template>
            будет удалён. Таски, ссылающиеся на него, перестанут запускаться (launch_failed).
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
