<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TabsItem } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import { deleteSecret, getSecretValue, listSecrets, setSecret } from '~/api/secret.api'
import { deleteVariable, listVariables, setVariable } from '~/api/variable.api'

// Переменные и секреты — одно env-пространство таска (и те, и другие
// инжектятся в под переменными окружения), поэтому показываем их единым
// списком: тип — колонка и фильтр, а не отдельный раздел меню. Скоупы:
// глобальный ('') и локальный для дага; локальный перекрывает глобальный
// при запуске. Значение переменной видно в списке, значение секрета — по
// кнопке (getSecretValue, RBAC).

type EnvKind = 'variable' | 'secret'

interface EnvEntry {
  kind: EnvKind
  name: string
  dag_name: string
  value?: string // только у переменных; у секретов значение тянется по кнопке
  created_at: string
  modified_at?: string
}

const kindMeta = {
  variable: {
    label: 'Переменная',
    plural: 'Переменные',
    icon: 'i-lucide-variable',
    color: 'neutral',
    hint: 'Значение видно всем аутентифицированным. Таск подключает переменную опцией loom.Variable(envName, name).',
  },
  secret: {
    label: 'Секрет',
    plural: 'Секреты',
    icon: 'i-lucide-key-round',
    color: 'warning',
    hint: 'Значение шифруется на сервере; посмотреть его можно только по кнопке в списке. Таск подключает секрет опцией loom.Secret(envName, name).',
  },
} as const

const route = useRoute()
const router = useRouter()
const { canManageDag } = useAuth()

// право менять запись: глобальные — только admin, локальные — владелец дага
function canManageScope(dagName: string): boolean {
  return canManageDag(dagName || undefined)
}

const entries = ref<EnvEntry[]>([])
const loading = ref(false)
const action = useApiAction()
const toast = useToast()

// фильтр скоупа: ALL_SCOPES — все, GLOBAL_SCOPE — глобальные, иначе имя дага
const ALL_SCOPES = '__all__'
// reka-ui Select не принимает '' как value опции — глобальный скоуп через sentinel
const GLOBAL_SCOPE = '__global__'
const ALL_KINDS = 'all'

const scopeFilter = ref<string>(
  typeof route.query.dag_name === 'string' ? (route.query.dag_name || GLOBAL_SCOPE) : ALL_SCOPES,
)
const kindFilter = ref<EnvKind | typeof ALL_KINDS>(
  route.query.kind === 'variable' || route.query.kind === 'secret' ? route.query.kind : ALL_KINDS,
)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const dagNames = ref<string[]>([])

const scopeItems = computed(() => [
  { label: 'Все скоупы', value: ALL_SCOPES },
  { label: 'Глобальные', value: GLOBAL_SCOPE },
  ...dagNames.value.map(n => ({ label: `Даг: ${n}`, value: n })),
])

// скоуп фильтруется на сервере (dag_name), тип и поиск — на клиенте
function scopeParam(): string | undefined {
  if (scopeFilter.value === ALL_SCOPES)
    return undefined
  return scopeFilter.value === GLOBAL_SCOPE ? '' : scopeFilter.value
}

async function load() {
  loading.value = true
  const dagName = scopeParam()
  try {
    const [variables, secrets] = await Promise.all([
      listVariables(dagName).catch((error) => {
        toast.add({ title: 'Ошибка загрузки переменных', description: apiErrorMessage(error), color: 'error' })
        return null
      }),
      listSecrets(dagName).catch((error) => {
        toast.add({ title: 'Ошибка загрузки секретов', description: apiErrorMessage(error), color: 'error' })
        return null
      }),
    ])

    entries.value = [
      ...(variables?.results ?? []).map<EnvEntry>(v => ({ kind: 'variable', ...v })),
      ...(secrets?.results ?? []).map<EnvEntry>(s => ({ kind: 'secret', ...s })),
    ].sort((a, b) =>
      a.name.localeCompare(b.name)
      || a.dag_name.localeCompare(b.dag_name)
      || a.kind.localeCompare(b.kind),
    )
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

// фильтры живут в URL — ссылку на срез («секреты дага X») можно передать
watch([kindFilter, scopeFilter, search], () => {
  const query: Record<string, string> = {}
  if (kindFilter.value !== ALL_KINDS)
    query.kind = kindFilter.value
  const dagName = scopeParam()
  if (dagName !== undefined)
    query.dag_name = dagName
  if (search.value)
    query.q = search.value
  router.replace({ query })
})

// поиск применяется до фильтра типа — счётчики во вкладках показывают,
// сколько найдётся при переключении
const searched = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q)
    return entries.value
  return entries.value.filter(e =>
    e.name.toLowerCase().includes(q) || e.dag_name.toLowerCase().includes(q),
  )
})

const filtered = computed(() =>
  kindFilter.value === ALL_KINDS ? searched.value : searched.value.filter(e => e.kind === kindFilter.value),
)

const kindItems = computed<TabsItem[]>(() => [
  { label: 'Всё', value: ALL_KINDS, badge: searched.value.length },
  {
    label: kindMeta.variable.plural,
    value: 'variable',
    icon: kindMeta.variable.icon,
    badge: searched.value.filter(e => e.kind === 'variable').length,
  },
  {
    label: kindMeta.secret.plural,
    value: 'secret',
    icon: kindMeta.secret.icon,
    badge: searched.value.filter(e => e.kind === 'secret').length,
  },
])

const hasFilters = computed(() =>
  kindFilter.value !== ALL_KINDS || scopeFilter.value !== ALL_SCOPES || search.value !== '',
)

function resetFilters() {
  kindFilter.value = ALL_KINDS
  scopeFilter.value = ALL_SCOPES
  search.value = ''
}

// создание / изменение
const editOpen = ref(false)
const editKind = ref<EnvKind>('variable')
const editScope = ref('')
const editName = ref('')
const editValue = ref('')
const editIsNew = ref(true)

const editScopeItems = computed(() => [
  { label: 'Глобальный', value: GLOBAL_SCOPE },
  ...dagNames.value.map(n => ({ label: `Даг: ${n}`, value: n })),
])

const editKindItems = computed<TabsItem[]>(() => [
  { label: kindMeta.variable.label, value: 'variable', icon: kindMeta.variable.icon },
  { label: kindMeta.secret.label, value: 'secret', icon: kindMeta.secret.icon },
])

const createItems = computed<DropdownMenuItem[][]>(() => [[
  {
    label: kindMeta.variable.label,
    icon: kindMeta.variable.icon,
    onSelect: () => openCreate('variable'),
  },
  {
    label: kindMeta.secret.label,
    icon: kindMeta.secret.icon,
    onSelect: () => openCreate('secret'),
  },
]])

function openCreate(kind: EnvKind) {
  editIsNew.value = true
  editKind.value = kind
  editScope.value = scopeFilter.value === ALL_SCOPES ? GLOBAL_SCOPE : scopeFilter.value
  editName.value = ''
  editValue.value = ''
  editOpen.value = true
}

// у переменной правим значение, у секрета — перезаписываем (старое не показываем)
function openEdit(entry: EnvEntry) {
  editIsNew.value = false
  editKind.value = entry.kind
  editScope.value = entry.dag_name || GLOBAL_SCOPE
  editName.value = entry.name
  editValue.value = entry.kind === 'variable' ? (entry.value ?? '') : ''
  editOpen.value = true
}

const editTitle = computed(() => {
  if (editIsNew.value)
    return editKind.value === 'variable' ? 'Создание переменной' : 'Создание секрета'
  return editKind.value === 'variable'
    ? `Переменная ${editName.value}`
    : `Замена значения ${editName.value}`
})

const canSubmitEdit = computed(() =>
  editName.value.trim() !== '' && (editKind.value === 'variable' || editValue.value !== ''),
)

async function submitEdit() {
  if (!canSubmitEdit.value)
    return
  const name = editName.value.trim()
  const dagName = editScope.value === GLOBAL_SCOPE ? '' : editScope.value
  const isVariable = editKind.value === 'variable'
  const ok = await action.run(
    () => isVariable ? setVariable(dagName, name, editValue.value) : setSecret(dagName, name, editValue.value),
    { success: isVariable ? 'Переменная сохранена' : 'Секрет сохранён' },
  )
  if (ok !== undefined) {
    editOpen.value = false
    editValue.value = ''
    await load()
  }
}

// значение секрета — по кнопке; ключ показанных: тип/скоуп/имя
const shownValues = ref<Record<string, string>>({})

function entryKey(e: EnvEntry): string {
  return `${e.kind} ${e.dag_name} ${e.name}`
}

function visibleValue(e: EnvEntry): string | undefined {
  return e.kind === 'variable' ? e.value : shownValues.value[entryKey(e)]
}

async function toggleValue(e: EnvEntry) {
  const key = entryKey(e)
  if (shownValues.value[key] !== undefined) {
    const { [key]: _hidden, ...rest } = shownValues.value
    shownValues.value = rest
    return
  }
  const rep = await action.run(() => getSecretValue(e.dag_name, e.name), { silent: true })
  if (rep)
    shownValues.value = { ...shownValues.value, [key]: rep.value }
}

async function copyValue(e: EnvEntry) {
  const value = visibleValue(e)
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

const deleteTarget = ref<EnvEntry | null>(null)

// склонения в подтверждении удаления («Секрет … будет удалён» / «Переменная … будет удалена»)
const deleteText = computed(() =>
  deleteTarget.value?.kind === 'secret'
    ? { noun: 'Секрет', verb: 'будет удалён', pronoun: 'него' }
    : { noun: 'Переменная', verb: 'будет удалена', pronoun: 'неё' },
)

async function confirmDelete() {
  const entry = deleteTarget.value
  if (!entry)
    return
  const isVariable = entry.kind === 'variable'
  const ok = await action.run(
    () => isVariable ? deleteVariable(entry.dag_name, entry.name) : deleteSecret(entry.dag_name, entry.name),
    { success: isVariable ? 'Переменная удалена' : 'Секрет удалён' },
  )
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<EnvEntry>[] = [
  { accessorKey: 'name', header: 'Имя' },
  { accessorKey: 'kind', header: 'Тип' },
  { accessorKey: 'dag_name', header: 'Скоуп' },
  { id: 'value', header: 'Значение' },
  { id: 'updated', header: 'Обновлено' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="env">
    <template #header>
      <UDashboardNavbar title="Переменные и секреты">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" aria-label="Обновить список" @click="load" />
          <UDropdownMenu :items="createItems">
            <UButton icon="i-lucide-plus" label="Создать" trailing-icon="i-lucide-chevron-down" />
          </UDropdownMenu>
        </template>
      </UDashboardNavbar>
      <UDashboardToolbar>
        <template #left>
          <UTabs
            v-model="kindFilter"
            :items="kindItems"
            :content="false"
            color="neutral"
            variant="pill"
            size="sm"
          />
          <USelect v-model="scopeFilter" :items="scopeItems" value-key="value" class="w-56" />
          <UInput
            v-model="search"
            icon="i-lucide-search"
            placeholder="Поиск по имени"
            class="w-56"
            :ui="{ trailing: 'pe-1' }"
          >
            <template v-if="search" #trailing>
              <UButton
                icon="i-lucide-x"
                size="xs"
                color="neutral"
                variant="ghost"
                aria-label="Очистить поиск"
                @click="search = ''"
              />
            </template>
          </UInput>
        </template>
      </UDashboardToolbar>
    </template>

    <template #body>
      <UTable :data="filtered" :columns="columns" :loading="loading" :ui="denseTableUi">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <UIcon
              :name="kindMeta[row.original.kind].icon"
              class="size-4 shrink-0"
              :class="row.original.kind === 'secret' ? 'text-warning' : 'text-muted'"
            />
            <span class="font-mono font-medium text-highlighted">{{ row.original.name }}</span>
          </div>
        </template>

        <template #kind-cell="{ row }">
          <UBadge :color="kindMeta[row.original.kind].color" variant="subtle" size="sm">
            {{ kindMeta[row.original.kind].label }}
          </UBadge>
        </template>

        <template #dag_name-cell="{ row }">
          <UBadge v-if="row.original.dag_name" color="info" variant="subtle" size="sm">
            {{ row.original.dag_name }}
          </UBadge>
          <UBadge v-else color="neutral" variant="subtle" size="sm">глобальный</UBadge>
        </template>

        <template #value-cell="{ row }">
          <div class="flex items-center gap-1">
            <template v-if="visibleValue(row.original) !== undefined">
              <span
                class="min-w-0 max-w-72 truncate font-mono text-xs"
                :title="visibleValue(row.original)"
              >
                {{ visibleValue(row.original) || '—' }}
              </span>
              <UTooltip text="Скопировать значение">
                <UButton icon="i-lucide-copy" size="xs" color="neutral" variant="ghost" aria-label="Скопировать значение" @click="copyValue(row.original)" />
              </UTooltip>
              <UTooltip v-if="row.original.kind === 'secret'" text="Скрыть">
                <UButton icon="i-lucide-eye-off" size="xs" color="neutral" variant="ghost" aria-label="Скрыть значение" @click="toggleValue(row.original)" />
              </UTooltip>
            </template>
            <template v-else>
              <span class="font-mono text-xs text-muted">••••••</span>
              <UTooltip text="Показать значение">
                <UButton icon="i-lucide-eye" size="xs" color="neutral" variant="ghost" aria-label="Показать значение" @click="toggleValue(row.original)" />
              </UTooltip>
            </template>
          </div>
        </template>

        <template #updated-cell="{ row }">
          <RelativeTime
            :time="row.original.modified_at ?? row.original.created_at"
            :tooltip="`Обновлено: ${formatDateTime(row.original.modified_at ?? row.original.created_at)} · Создано: ${formatDateTime(row.original.created_at)}`"
          />
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip
              v-if="canManageScope(row.original.dag_name)"
              :text="row.original.kind === 'variable' ? 'Изменить значение' : 'Заменить значение'"
            >
              <UButton
                :icon="row.original.kind === 'variable' ? 'i-lucide-pencil' : 'i-lucide-rotate-ccw-key'"
                size="sm"
                color="neutral"
                variant="ghost"
                :aria-label="row.original.kind === 'variable' ? 'Изменить значение' : 'Заменить значение'"
                @click="openEdit(row.original)"
              />
            </UTooltip>
            <UTooltip v-if="canManageScope(row.original.dag_name)" text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" aria-label="Удалить" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>

        <template #empty>
          <div class="py-6 text-center text-muted">
            <template v-if="hasFilters">
              <p>Ничего не найдено.</p>
              <UButton class="mt-2" size="sm" color="neutral" variant="subtle" label="Сбросить фильтры" @click="resetFilters" />
            </template>
            <template v-else>
              Переменных и секретов нет. Таск подключает их опциями
              <span class="font-mono">loom.Variable(envName, name)</span> и
              <span class="font-mono">loom.Secret(envName, name)</span>.
            </template>
          </div>
        </template>
      </UTable>

      <!-- создание / изменение -->
      <UModal v-model:open="editOpen" :title="editTitle" :description="kindMeta[editKind].hint">
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="editIsNew" label="Тип">
              <UTabs
                v-model="editKind"
                :items="editKindItems"
                :content="false"
                color="neutral"
                variant="pill"
                size="sm"
                class="w-full"
              />
            </UFormField>
            <UFormField v-if="editIsNew" label="Скоуп" hint="локальный перекрывает глобальный с тем же именем">
              <USelect v-model="editScope" :items="editScopeItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField v-if="editIsNew" :label="editKind === 'variable' ? 'Имя переменной' : 'Имя секрета'">
              <UInput
                v-model="editName"
                class="w-full"
                :placeholder="editKind === 'variable' ? 'api-base-url' : 'prod-db-password'"
                autofocus
              />
            </UFormField>
            <UFormField
              label="Значение"
              :hint="!editIsNew && editKind === 'secret' ? 'старое значение не показывается' : undefined"
            >
              <UTextarea v-model="editValue" class="w-full font-mono" :rows="3" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
            <UButton label="Сохранить" :disabled="!canSubmitEdit" :loading="action.loading.value" @click="submitEdit" />
          </div>
        </template>
      </UModal>

      <!-- подтверждение удаления -->
      <UModal
        :open="deleteTarget !== null"
        :title="deleteTarget?.kind === 'secret' ? 'Удалить секрет?' : 'Удалить переменную?'"
        @update:open="deleteTarget = null"
      >
        <template #body>
          <p>
            {{ deleteText.noun }}
            <span class="font-mono font-medium">{{ deleteTarget?.name }}</span>
            <template v-if="deleteTarget?.dag_name">
              (даг <span class="font-mono">{{ deleteTarget?.dag_name }}</span>)
            </template>
            {{ deleteText.verb }}. Таски, ссылающиеся на {{ deleteText.pronoun }}, перестанут
            запускаться (launch_failed).
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
