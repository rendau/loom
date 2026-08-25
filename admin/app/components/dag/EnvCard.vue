<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { setSecret } from '~/api/secret.api'
import { setVariable } from '~/api/variable.api'
import type { DagEnvRequirement } from '~/utils/dagenv'

// Что даг требует от окружения: переменные и секреты, объявленные в его
// коде (манифест describe), рядом — заведено ли значение и в каком скоупе.
// Заполнить можно прямо отсюда: глобально (значение увидят все даги) или
// локально для дага (перекрывает глобальное). Данные грузит карточка дага
// (useDagEnvRequirements) — счётчик «не заполнено» нужен ей самой для бейджа.

const props = defineProps<{
  dagName: string
  requirements: DagEnvRequirement[]
  loading?: boolean
  loadError?: string
}>()

const emit = defineEmits<{ reload: [] }>()

const { isAdmin, canManageDag } = useAuth()

const action = useApiAction()

const missing = computed(() => countMissingEnv(props.requirements))

// тип — иконкой в имени (колонка ради двух слов съедала место), скоуп —
// бейджем после имени: колонка «Значение» остаётся только под значение
const columns: TableColumn<DagEnvRequirement>[] = [
  { id: 'name', header: 'Переменная' },
  { id: 'value', header: 'Значение' },
  { id: 'usage', header: 'Таски' },
  { id: 'actions', header: '' },
]

// право заполнить в конкретном скоупе: глобальный — только admin,
// локальный — владелец дага
function canManageScope(scope: string): boolean {
  return canManageDag(scope || undefined)
}

const canFillSomething = computed(() => isAdmin.value || canManageDag(props.dagName))

// ── заполнение значения ────────────────────────────────

const GLOBAL_SCOPE = '__global__'

const editOpen = ref(false)
const editTarget = ref<DagEnvRequirement | null>(null)
const editScope = ref<string>(GLOBAL_SCOPE)
const editValue = ref('')

const editScopeItems = computed(() => [
  { label: 'Глобально — для всех дагов', value: GLOBAL_SCOPE, disabled: !isAdmin.value },
  { label: `Локально — только для дага ${props.dagName}`, value: props.dagName, disabled: !canManageDag(props.dagName) },
])

function openEdit(req: DagEnvRequirement) {
  editTarget.value = req
  // уже заполненное правим в его же скоупе, новое — в том, где есть права
  // (по умолчанию локально: значение одного дага реже задевает соседей)
  editScope.value = req.scope === '' ? GLOBAL_SCOPE : req.scope ?? (canManageDag(props.dagName) ? props.dagName : GLOBAL_SCOPE)
  editValue.value = req.kind === 'variable' && req.scope !== undefined ? (req.value ?? '') : ''
  editOpen.value = true
}

const editTitle = computed(() => {
  const req = editTarget.value
  if (!req)
    return ''
  const noun = req.kind === 'variable' ? 'переменной' : 'секрета'
  return `${req.scope === undefined ? 'Заполнение' : 'Изменение'} ${noun} ${req.name}`
})

const canSubmit = computed(() => {
  const req = editTarget.value
  if (!req)
    return false
  // у секрета пустое значение бессмысленно, у переменной — легитимно
  return req.kind === 'variable' || editValue.value !== ''
})

async function submit() {
  const req = editTarget.value
  if (!req || !canSubmit.value)
    return
  const scope = editScope.value === GLOBAL_SCOPE ? '' : editScope.value
  const isVariable = req.kind === 'variable'
  const ok = await action.run(
    () => isVariable ? setVariable(scope, req.name, editValue.value) : setSecret(scope, req.name, editValue.value),
    { success: isVariable ? 'Переменная сохранена' : 'Секрет сохранён' },
  )
  if (ok !== undefined) {
    editOpen.value = false
    editValue.value = ''
    emit('reload')
  }
}

// ссылка на запись в общем разделе /env (там правка, удаление, история)
function envLink(req: DagEnvRequirement): string {
  const query = new URLSearchParams({ kind: req.kind, q: req.name })
  if (req.scope)
    query.set('dag_name', req.scope)
  return `/env?${query.toString()}`
}
</script>

<template>
  <section>
    <SectionHeader title="Переменные и секреты дага" :count="props.requirements.length" />

    <UAlert
      v-if="props.loadError"
      class="mb-2"
      color="error"
      variant="subtle"
      title="Ошибка загрузки значений"
      :description="props.loadError"
      :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => emit('reload') }]"
    />

    <UAlert
      v-else-if="missing > 0"
      class="mb-2"
      color="error"
      variant="subtle"
      icon="i-lucide-triangle-alert"
      :title="`Не заполнено: ${missing}`"
      description="Пока значения нет, запуск таска упадёт с launch_failed. Заполните глобально (значение увидят все даги) или локально для этого дага."
    />

    <UCard :ui="{ body: 'p-0 sm:p-0' }">
      <UTable :data="props.requirements" :columns="columns" :loading="props.loading" :ui="denseTableUi">
        <template #name-cell="{ row }">
          <div class="flex min-w-0 items-start gap-2">
            <UTooltip
              :text="`${row.original.kind === 'secret' ? 'секрет' : 'переменная'} · в контейнере: ${row.original.envs.join(', ')}`"
            >
              <UIcon
                :name="row.original.kind === 'secret' ? 'i-lucide-key-round' : 'i-lucide-variable'"
                class="mt-0.5 size-3.5 shrink-0"
                :class="row.original.kind === 'secret' ? 'text-warning' : 'text-dimmed'"
              />
            </UTooltip>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-1.5">
                <NuxtLink
                  :to="envLink(row.original)"
                  class="font-mono text-xs font-medium text-highlighted hover:text-primary hover:underline"
                >
                  {{ row.original.name }}
                </NuxtLink>
                <UBadge v-if="row.original.scope === ''" color="neutral" variant="subtle" size="sm">
                  глобальная
                </UBadge>
                <UBadge v-else-if="row.original.scope !== undefined" color="info" variant="subtle" size="sm">
                  локальная
                </UBadge>
              </div>
              <p v-if="row.original.description" class="mt-0.5 max-w-100 text-xs text-muted">
                {{ row.original.description }}
              </p>
            </div>
          </div>
        </template>

        <template #value-cell="{ row }">
          <UBadge v-if="row.original.scope === undefined" color="error" variant="subtle" size="sm">
            не заполнена
          </UBadge>
          <span
            v-else-if="row.original.kind === 'secret'"
            class="font-mono text-xs text-muted"
          >••••••</span>
          <span
            v-else
            class="block max-w-80 truncate font-mono text-xs font-medium text-highlighted"
            :title="row.original.value"
          >{{ row.original.value || '—' }}</span>
        </template>

        <template #usage-cell="{ row }">
          <span class="text-xs text-muted">{{ row.original.tasks.join(', ') }}</span>
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end">
            <UButton
              v-if="canFillSomething"
              size="sm"
              :color="row.original.scope === undefined ? 'primary' : 'neutral'"
              :variant="row.original.scope === undefined ? 'soft' : 'ghost'"
              :icon="row.original.scope === undefined ? 'i-lucide-plus' : 'i-lucide-pencil'"
              :label="row.original.scope === undefined ? 'Заполнить' : undefined"
              :disabled="row.original.scope !== undefined && !canManageScope(row.original.scope)"
              :aria-label="row.original.scope === undefined ? 'Заполнить' : 'Изменить значение'"
              @click="openEdit(row.original)"
            />
          </div>
        </template>

        <template #empty>
          <div class="py-6 text-center text-sm text-muted">
            Даг не объявляет переменных и секретов. Подключаются в коде таска опциями
            <span class="font-mono">loom.Variable(envName, name, "описание")</span> и
            <span class="font-mono">loom.Secret(envName, name, "описание")</span>.
          </div>
        </template>
      </UTable>
    </UCard>

    <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
      <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
      <span>
        Состав задаёт код дага (обновляется при регистрации образа), значения — админка.
        Локальное значение перекрывает глобальное с тем же именем. Полный список записей —
        в разделе <NuxtLink to="/env" class="text-primary hover:underline">Переменные и секреты</NuxtLink>.
      </span>
    </p>

    <UModal v-model:open="editOpen" :title="editTitle" :description="editTarget?.description || undefined">
      <template #body>
        <div class="space-y-4">
          <UFormField label="Скоуп" hint="локальный перекрывает глобальный">
            <USelect v-model="editScope" :items="editScopeItems" value-key="value" class="w-full" />
          </UFormField>
          <UFormField
            label="Значение"
            :hint="editTarget?.kind === 'secret' ? 'значение шифруется; старое не показывается' : undefined"
          >
            <UTextarea v-model="editValue" class="w-full font-mono" :rows="3" autofocus />
          </UFormField>
          <p v-if="editTarget" class="text-xs text-muted">
            В контейнере таска будет доступна как
            <span class="font-mono">{{ editTarget.envs.join(', ') }}</span>.
          </p>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
          <UButton label="Сохранить" :disabled="!canSubmit" :loading="action.loading.value" @click="submit" />
        </div>
      </template>
    </UModal>
  </section>
</template>
