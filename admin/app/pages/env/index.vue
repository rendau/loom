<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TabsItem } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import type { Scope } from '~/types/common'
import { listDags } from '~/api/dag.api'
import { listProjects } from '~/api/project.api'
import { deleteSecret, getSecretValue, listSecrets, moveSecret, setSecret } from '~/api/secret.api'
import { deleteVariable, listVariables, moveVariable, setVariable } from '~/api/variable.api'

// Переменные и секреты — одно env-пространство таска (и те, и другие
// инжектятся в под переменными окружения), поэтому показываем их единым
// списком: тип — колонка и фильтр, а не отдельный раздел меню.
//
// У существующей записи правятся и тип, и скоуп. Смена скоупа — серверный
// перенос (Move): значение переезжает как есть, поэтому секрет не нужно
// вводить заново. Смена типа — другая сущность, её переносим здесь:
// создаём запись нового типа и удаляем старую. Скоупы
// трёхуровневые: глобальный, проекта и дага — более узкий перекрывает
// более широкий при запуске. Значение переменной видно в списке, значение секрета — по
// кнопке (getSecretValue, RBAC).

type EnvKind = 'variable' | 'secret'

interface EnvEntry {
  kind: EnvKind
  name: string
  scope: Scope
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
const { isAdmin, canManageDag, canManageProject } = useAuth()

// право менять запись: глобальные — только admin, проектные — владелец
// проекта, записи дага — владелец дага
function canManageScope(scope: Scope): boolean {
  switch (scopeKind(scope)) {
    case 'dag':
      return canManageDag({ project: scope.project, name: scope.dag })
    case 'project':
      return canManageProject(scope.project)
    default:
      return isAdmin.value
  }
}

const entries = ref<EnvEntry[]>([])
const loading = ref(false)
const action = useApiAction()
const toast = useToast()

// фильтр скоупа: ALL_SCOPES — все, GLOBAL_SCOPE — глобальные, иначе метка
// скоупа — «проект» или «проект/даг» (она же уезжает в query)
const ALL_SCOPES = '__all__'
// reka-ui Select не принимает '' как value опции — глобальный скоуп через sentinel
const GLOBAL_SCOPE = '__global__'
const ALL_KINDS = 'all'

const scopeFilter = ref<string>(
  typeof route.query.scope === 'string' ? (route.query.scope || GLOBAL_SCOPE) : ALL_SCOPES,
)
const kindFilter = ref<EnvKind | typeof ALL_KINDS>(
  route.query.kind === 'variable' || route.query.kind === 'secret' ? route.query.kind : ALL_KINDS,
)
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
// метки скоупов для селектов: проекты и их даги
const projectNames = ref<string[]>([])
const dagLabels = ref<string[]>([])

const scopeItems = computed(() => [
  { label: 'Все скоупы', value: ALL_SCOPES },
  { label: 'Глобальные', value: GLOBAL_SCOPE },
  ...projectNames.value.map(n => ({ label: `Проект: ${n}`, value: n })),
  ...dagLabels.value.map(n => ({ label: `Даг: ${n}`, value: n })),
])

// скоуп фильтруется на сервере, тип и поиск — на клиенте
function scopeParam(): Scope | undefined {
  if (scopeFilter.value === ALL_SCOPES)
    return undefined
  return scopeFilter.value === GLOBAL_SCOPE ? globalScope : parseScopeLabel(scopeFilter.value)
}

async function load() {
  loading.value = true
  const scope = scopeParam()
  try {
    const [variables, secrets] = await Promise.all([
      listVariables(scope).catch((error) => {
        toast.add({ title: 'Ошибка загрузки переменных', description: apiErrorMessage(error), color: 'error' })
        return null
      }),
      listSecrets(scope).catch((error) => {
        toast.add({ title: 'Ошибка загрузки секретов', description: apiErrorMessage(error), color: 'error' })
        return null
      }),
    ])

    entries.value = [
      ...(variables?.results ?? []).map<EnvEntry>(v => ({ kind: 'variable', ...v })),
      ...(secrets?.results ?? []).map<EnvEntry>(s => ({ kind: 'secret', ...s })),
    ].sort((a, b) =>
      a.name.localeCompare(b.name)
      || scopeLabel(a.scope).localeCompare(scopeLabel(b.scope))
      || a.kind.localeCompare(b.kind),
    )
  }
  finally {
    loading.value = false
  }
}

async function loadScopeNames() {
  try {
    const [dags, projects] = await Promise.all([
      listDags({ list_params: { page_size: 500, sort: ['project_name', 'name'] } }),
      listProjects({ list_params: { page_size: 200, sort: ['name'] } }),
    ])
    dagLabels.value = (dags.results ?? []).map(d => dagRefLabel(d))
    projectNames.value = (projects.results ?? []).map(p => p.name)
  }
  catch {
    // фильтр по проектам и дагам просто останется без вариантов
  }
}

onMounted(async () => {
  await Promise.all([load(), loadScopeNames()])
})
watch(scopeFilter, load)

// фильтры живут в URL — ссылку на срез («секреты дага X») можно передать
watch([kindFilter, scopeFilter, search], () => {
  const query: Record<string, string> = {}
  if (kindFilter.value !== ALL_KINDS)
    query.kind = kindFilter.value
  const scope = scopeParam()
  if (scope !== undefined)
    query.scope = scopeLabel(scope)
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
    e.name.toLowerCase().includes(q) || scopeLabel(e.scope).toLowerCase().includes(q),
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
// исходная запись: тип и скоуп можно поменять, и тогда сохранение — это
// перенос (запись создаётся в новом месте и удаляется из старого)
const editOrigin = ref<{ kind: EnvKind, scope: string } | null>(null)

// скоупы, где вызывающий вправе менять записи: глобальный — только admin,
// проектный — владелец проекта, дага — владелец дага
const editScopeItems = computed(() => [
  { label: 'Глобальный', value: GLOBAL_SCOPE, scope: globalScope },
  ...projectNames.value.map(n => ({ label: `Проект: ${n}`, value: n, scope: projectScope(n) })),
  ...dagLabels.value.map(n => ({ label: `Даг: ${n}`, value: n, scope: parseScopeLabel(n) })),
].filter(item => canManageScope(item.scope)))

const editKindItems = computed<TabsItem[]>(() => [
  { label: kindMeta.variable.label, value: 'variable', icon: kindMeta.variable.icon },
  { label: kindMeta.secret.label, value: 'secret', icon: kindMeta.secret.icon },
])

// сменили тип существующей записи: переменная и секрет — разные сущности,
// перенести одним запросом нельзя
const editKindChanged = computed(() =>
  !editIsNew.value && editOrigin.value !== null && editOrigin.value.kind !== editKind.value)

// сменили только скоуп — это Move на сервере
const editScopeChanged = computed(() =>
  !editIsNew.value && editOrigin.value !== null && editOrigin.value.scope !== editScope.value)

const editMoved = computed(() => editKindChanged.value || editScopeChanged.value)

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
  editOrigin.value = null
  editKind.value = kind
  // скоуп по умолчанию — из фильтра, но только если вызывающий вправе в нём
  // писать; иначе первый доступный (у обычного пользователя глобального нет)
  const preset = scopeFilter.value === ALL_SCOPES ? GLOBAL_SCOPE : scopeFilter.value
  editScope.value = editScopeItems.value.some(i => i.value === preset)
    ? preset
    : (editScopeItems.value[0]?.value ?? GLOBAL_SCOPE)
  editName.value = ''
  editValue.value = ''
  editOpen.value = true
}

// у переменной правим значение, у секрета — перезаписываем (старое не показываем)
function openEdit(entry: EnvEntry) {
  editIsNew.value = false
  editKind.value = entry.kind
  editScope.value = scopeKind(entry.scope) === 'global' ? GLOBAL_SCOPE : scopeLabel(entry.scope)
  editOrigin.value = { kind: editKind.value, scope: editScope.value }
  editName.value = entry.name
  editValue.value = entry.kind === 'variable' ? (entry.value ?? '') : ''
  editOpen.value = true
}

const editTitle = computed(() => {
  if (editIsNew.value)
    return editKind.value === 'variable' ? 'Создание переменной' : 'Создание секрета'
  if (editMoved.value)
    return `Перенос ${editName.value}`
  return editKind.value === 'variable'
    ? `Переменная ${editName.value}`
    : `Замена значения ${editName.value}`
})

// Значение обязательно у секрета, когда запись создаётся или меняет тип:
// текущее значение форме неизвестно (сервер его не отдаёт). При переносе
// секрета между скоупами значение не нужно — его двигает сервер.
const secretValueRequired = computed(() =>
  editKind.value === 'secret' && (editIsNew.value || editKindChanged.value))

const canSubmitEdit = computed(() =>
  editName.value.trim() !== '' && (!secretValueRequired.value || editValue.value !== ''),
)

function scopeOf(label: string) {
  return label === GLOBAL_SCOPE ? globalScope : parseScopeLabel(label)
}

async function submitEdit() {
  if (!canSubmitEdit.value)
    return
  const name = editName.value.trim()
  const scope = scopeOf(editScope.value)
  const isVariable = editKind.value === 'variable'
  const origin = editOrigin.value
  const kindChanged = editKindChanged.value
  const scopeChanged = editScopeChanged.value

  const ok = await action.run(
    async () => {
      // сменился только скоуп — перенос делает сервер, значение остаётся
      if (scopeChanged && !kindChanged && origin) {
        const from = scopeOf(origin.scope)
        if (isVariable)
          await moveVariable(from, scope, name)
        else
          await moveSecret(from, scope, name)
        // у переменной заодно могли поправить значение
        if (isVariable && editValue.value !== '')
          await setVariable(scope, name, editValue.value)
        return true
      }

      // сменился тип (возможно, вместе со скоупом): создаём запись нового
      // типа и удаляем старую — сначала создание, чтобы при ошибке старая
      // осталась
      if (isVariable)
        await setVariable(scope, name, editValue.value)
      else
        await setSecret(scope, name, editValue.value)

      if (kindChanged && origin) {
        const from = scopeOf(origin.scope)
        if (origin.kind === 'variable')
          await deleteVariable(from, name)
        else
          await deleteSecret(from, name)
      }
      return true
    },
    {
      success: kindChanged
        ? 'Тип записи изменён'
        : scopeChanged ? 'Запись перенесена' : (isVariable ? 'Переменная сохранена' : 'Секрет сохранён'),
    },
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
  return `${e.kind} ${scopeLabel(e.scope)} ${e.name}`
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
  // ошибка (нет прав, секрет не расшифровался) — обычным тостом, иначе
  // клик по глазу молча ничего не делает
  const rep = await action.run(() => getSecretValue(e.scope, e.name))
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
    () => isVariable ? deleteVariable(entry.scope, entry.name) : deleteSecret(entry.scope, entry.name),
    { success: isVariable ? 'Переменная удалена' : 'Секрет удалён' },
  )
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

// тип — иконкой слева от имени (и фильтром-вкладкой сверху): колонка ради
// одного слова съедала место, которое нужнее значению
const columns: TableColumn<EnvEntry>[] = [
  { accessorKey: 'name', header: 'Имя' },
  { id: 'value', header: 'Значение' },
  { id: 'scope', header: 'Скоуп' },
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
          <!-- создавать некуда — у вызывающего нет ни одного скоупа с правом записи -->
          <UDropdownMenu v-if="editScopeItems.length > 0" :items="createItems">
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
            <UTooltip :text="kindMeta[row.original.kind].label">
              <UIcon
                :name="kindMeta[row.original.kind].icon"
                class="size-4 shrink-0"
                :class="row.original.kind === 'secret' ? 'text-warning' : 'text-muted'"
              />
            </UTooltip>
            <span class="font-mono font-medium text-highlighted">{{ row.original.name }}</span>
          </div>
        </template>

        <template #scope-cell="{ row }">
          <UBadge
            v-if="scopeKind(row.original.scope) !== 'global'"
            :color="scopeKind(row.original.scope) === 'dag' ? 'info' : 'primary'"
            variant="subtle"
            size="sm"
          >
            {{ scopeLabel(row.original.scope) }}
          </UBadge>
          <UBadge v-else color="neutral" variant="subtle" size="sm">глобальный</UBadge>
        </template>

        <template #value-cell="{ row }">
          <div class="flex items-center gap-1">
            <template v-if="visibleValue(row.original) !== undefined">
              <span
                class="min-w-0 max-w-72 truncate font-mono text-xs font-medium text-highlighted"
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
              <!-- значение секрета отдаётся только владельцу скоупа — чужим
                   глаз не показываем, сервер всё равно откажет -->
              <UTooltip v-if="canManageScope(row.original.scope)" text="Показать значение">
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
              v-if="canManageScope(row.original.scope)"
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
            <UTooltip v-if="canManageScope(row.original.scope)" text="Удалить">
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
            <UFormField label="Тип">
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
            <UFormField label="Скоуп" hint="более узкий перекрывает более широкий с тем же именем">
              <USelect v-model="editScope" :items="editScopeItems" value-key="value" class="w-full" />
            </UFormField>
            <!-- смена типа или скоупа существующей записи — перенос:
                 создаём на новом месте и удаляем со старого -->
            <UAlert
              v-if="editMoved"
              color="info"
              variant="subtle"
              icon="i-lucide-arrow-right-left"
              :title="editKindChanged
                ? `Запись станет ${editKind === 'secret' ? 'секретом' : 'переменной'}`
                : `Запись будет перенесена в «${editScopeItems.find(i => i.value === editScope)?.label ?? editScope}»`"
              :description="editKindChanged
                ? (editKind === 'secret'
                  ? 'Тип меняется на секрет: введите значение — сервер хранит секреты зашифрованными и старое значение переменной не переносит.'
                  : 'Тип меняется на переменную: значение секрета сервер не отдаёт, введите его заново.')
                : 'Значение переезжает вместе с записью — вводить его заново не нужно.'"
            />
            <UFormField v-if="editIsNew" :label="editKind === 'variable' ? 'Имя переменной' : 'Имя секрета'">
              <UInput
                v-model="editName"
                class="w-full"
                :placeholder="editKind === 'variable' ? 'api-base-url' : 'prod-db-password'"
                autofocus
              />
            </UFormField>
            <!-- при переносе секрета между скоупами значение не трогаем:
                 его двигает сервер, поле только сбило бы с толку -->
            <UFormField
              v-if="!(editKind === 'secret' && editScopeChanged && !editKindChanged)"
              label="Значение"
              :hint="!editIsNew && editKind === 'secret'
                ? (secretValueRequired ? 'введите новое значение' : 'старое значение не показывается')
                : undefined"
            >
              <UTextarea v-model="editValue" class="w-full font-mono" :rows="3" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
            <UButton
              :label="editMoved ? 'Перенести' : 'Сохранить'"
              :disabled="!canSubmitEdit"
              :loading="action.loading.value"
              @click="submitEdit"
            />
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
            <template v-if="deleteTarget && scopeKind(deleteTarget.scope) !== 'global'">
              ({{ scopeKind(deleteTarget.scope) === 'dag' ? 'даг' : 'проект' }}
              <span class="font-mono">{{ scopeLabel(deleteTarget.scope) }}</span>)
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
