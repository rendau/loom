<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import { createDag, listDags } from '~/api/dag.api'
import { getProject, listProjectRegistrations, setProjectAutoUpdate, syncProject } from '~/api/project.api'
import { listPools } from '~/api/pool.api'
import type { Dag } from '~/types/dag'
import type { Pool } from '~/types/pool'
import type { Project, ProjectRegistration, ProjectTemplate } from '~/types/project'

// Карточка проекта: образ и его каталог (даги, объявленные в коде) плюс
// заведённые от них даги-инстансы. Отсюда заводят новый инстанс — от
// одного шаблона их может быть сколько угодно, различаются они
// настройками, переменными и секретами.

const route = useRoute()
const projectName = String(route.params.name)

const { canManageProject } = useAuth()
const canManage = computed(() => canManageProject(projectName))

const project = ref<Project | null>(null)
const dags = ref<Dag[]>([])
const registrations = ref<ProjectRegistration[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

const crumbs = computed(() => [
  { label: 'Проекты', icon: 'i-lucide-package', to: '/projects' },
  { label: projectName },
])

const templates = computed(() => project.value?.templates ?? [])

// SDK — свойство образа, а не отдельного дага: describe отдаёт версию на
// каталог, поэтому у всех шаблонов она одна. Разойтись значения могут
// только у старой записи, пережившей перерегистрацию, — тогда покажем все.
const sdkVersions = computed(() =>
  [...new Set(templates.value.map(t => t.sdk_version).filter(Boolean))])

// сколько инстансов заведено от каждого шаблона — из списка дагов проекта
const dagsByTemplate = computed(() => {
  const out = new Map<string, Dag[]>()
  for (const d of dags.value)
    out.set(d.template, [...(out.get(d.template) ?? []), d])
  return out
})

const activeRegistrations = computed(() =>
  registrations.value.filter(r => r.status === 'pending' || r.status === 'running'))

const { failed: failedRegistrations, dismiss: dismissFailed } = useRegistrationFailures(registrations)

async function load() {
  loading.value = true
  try {
    const [p, d, regs] = await Promise.all([
      getProject(projectName),
      listDags({ list_params: { page_size: 200, sort: ['name'] }, project: projectName }),
      listProjectRegistrations({ project_name: projectName, limit: 20 }),
    ])
    project.value = p
    dags.value = d.results ?? []
    registrations.value = regs.results ?? []
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

// пока регистрация идёт — обновляем карточку: шаблоны и даги меняются
usePolling(async () => {
  if (activeRegistrations.value.length === 0)
    return
  await load()
}, 3000)

async function toggleAutoUpdate() {
  const p = project.value
  if (!p)
    return
  const ok = await action.run(
    () => setProjectAutoUpdate(p.name, !p.auto_update),
    { success: p.auto_update ? 'Авто-обновление выключено' : 'Авто-обновление включено' },
  )
  if (ok !== undefined)
    await load()
}

async function sync() {
  const ok = await action.run(
    () => syncProject(projectName),
    { success: 'Обновление из registry поставлено в очередь' },
  )
  if (ok !== undefined)
    await load()
}

// ── заведение дага от шаблона ──────────────────────────

// reka-ui Select не принимает '' как value — «пул не задан» через sentinel
const NO_POOL = '__none__'

const createOpen = ref(false)
const createTemplate = ref<ProjectTemplate | null>(null)
const createName = ref('')
const createSchedule = ref('')
const createCatchup = ref(false)
const createStartScheduled = ref(true)
const createPool = ref(NO_POOL)
const pools = ref<Pool[]>([])

const poolItems = computed(() => [
  { label: 'default (общий пул)', value: NO_POOL },
  ...pools.value.filter(p => p.name !== 'default').map(p => ({ label: `${p.name} · ${p.slots} слотов`, value: p.name })),
])

async function loadPools() {
  try {
    pools.value = (await listPools()).results ?? []
  }
  catch {
    // селект останется с одним «не задан» — даг всё равно заводится
  }
}

onMounted(loadPools)

function openCreate(template: ProjectTemplate) {
  createTemplate.value = template
  // первый инстанс называется как даг в образе, следующим нужно своё имя
  const taken = dagsByTemplate.value.get(template.name)?.length ?? 0
  createName.value = taken === 0 ? template.name : ''
  createSchedule.value = ''
  createCatchup.value = false
  createStartScheduled.value = true
  createPool.value = NO_POOL
  createOpen.value = true
}

const createNameTaken = computed(() =>
  dags.value.some(d => d.name === createName.value.trim()))

const canSubmitCreate = computed(() =>
  createTemplate.value !== null && createName.value.trim() !== '' && !createNameTaken.value)

async function submitCreate() {
  const template = createTemplate.value
  if (!template || !canSubmitCreate.value)
    return

  const schedule = createSchedule.value.trim()
  const created = await action.run(
    () => createDag({
      project: projectName,
      template: template.name,
      name: createName.value.trim(),
      schedule: schedule || undefined,
      catchup: schedule ? createCatchup.value : undefined,
      paused: schedule ? !createStartScheduled.value : undefined,
      pool: createPool.value === NO_POOL ? undefined : createPool.value,
    }),
    { success: 'Даг заведён' },
  )
  if (created === undefined)
    return

  createOpen.value = false
  await load()
}

const templateColumns: TableColumn<ProjectTemplate>[] = [
  { accessorKey: 'name', header: 'Даг в образе' },
  { id: 'tasks', header: 'Тасков' },
  { id: 'dags', header: 'Заведено' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="project">
    <template #header>
      <UDashboardNavbar :title="projectName">
        <template #leading>
          <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" to="/projects" aria-label="К проектам" />
        </template>
        <template #title>
          <PageCrumbs :items="crumbs" kind="проект" />
        </template>
        <template #right>
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить"
            @click="load"
          />
          <UButton
            v-if="canManage"
            icon="i-lucide-cloud-download"
            color="neutral"
            variant="subtle"
            label="Обновить из registry"
            :disabled="activeRegistrations.length > 0"
            :loading="action.loading.value"
            @click="sync"
          />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки проекта"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <DagRegistrationQueue
        :active="activeRegistrations"
        :failed="failedRegistrations"
        @dismiss-failed="dismissFailed"
      />

      <template v-if="project">
        <MetaGrid>
          <MetaItem label="Образ">
            <CopyText :text="project.image" />
          </MetaItem>
          <MetaItem label="Digest">
            <CopyText :text="project.image_digest" />
          </MetaItem>
          <MetaItem label="Размер образа">
            <!-- как он лежит в registry (сжатым) — столько тянет pull -->
            <span v-if="Number(project.image_size_bytes)" class="tabular-nums">
              {{ formatBytes(project.image_size_bytes) }}
            </span>
            <UTooltip v-else text="Registry не удалось опросить при последней регистрации">
              <span class="text-muted">—</span>
            </UTooltip>
          </MetaItem>
          <MetaItem label="SDK">
            <span v-if="sdkVersions.length" class="font-mono">{{ sdkVersions.join(', ') }}</span>
            <span v-else class="text-muted">—</span>
          </MetaItem>
          <MetaItem label="Дагов в образе">
            {{ templates.length }}
          </MetaItem>
          <MetaItem label="Заведено дагов">
            {{ dags.length }}
          </MetaItem>
          <MetaItem label="Авто-обновление">
            <!-- вкл/выкл — свойство проекта, а не действие: переключатель
                 показывает состояние и меняет его одним кликом -->
            <USwitch
              v-if="canManage"
              :model-value="project.auto_update"
              :disabled="action.loading.value"
              :label="project.auto_update ? 'включено' : 'выключено'"
              :ui="{ label: 'text-sm text-default' }"
              @update:model-value="toggleAutoUpdate"
            />
            <UBadge v-else :color="project.auto_update ? 'info' : 'neutral'" variant="subtle" size="sm">
              {{ project.auto_update ? 'включено' : 'выключено' }}
            </UBadge>
          </MetaItem>
          <MetaItem label="Обновлён">
            <RelativeTime :time="project.modified_at ?? project.created_at" />
          </MetaItem>
        </MetaGrid>

        <section class="mt-4">
          <SectionHeader title="Даги образа" :count="templates.length" />

          <UCard :ui="{ body: 'p-0 sm:p-0' }">
            <UTable :data="templates" :columns="templateColumns" :loading="loading" :ui="denseTableUi">
              <template #name-cell="{ row }">
                <div class="flex items-center gap-2">
                  <!-- страница шаблона: граф, таски и требования к окружению -->
                  <NuxtLink
                    :to="templateLink(projectName, row.original.name)"
                    class="font-mono font-medium text-highlighted hover:text-primary hover:underline"
                  >
                    {{ row.original.name }}
                  </NuxtLink>
                  <UTooltip
                    v-if="row.original.orphaned"
                    text="Дага нет в последней версии образа — заведённые инстансы работают на последнем известном манифесте"
                  >
                    <UBadge color="warning" variant="subtle" size="sm">исчез из образа</UBadge>
                  </UTooltip>
                </div>
              </template>

              <template #tasks-cell="{ row }">
                {{ row.original.tasks?.length ?? 0 }}
              </template>

              <template #dags-cell="{ row }">
                <!-- инстансов у шаблона может быть много: перечисление
                     через запятую, иначе имена сливаются в одну строку -->
                <div v-if="dagsByTemplate.get(row.original.name)?.length" class="flex flex-wrap items-baseline">
                  <template v-for="(dag, i) in dagsByTemplate.get(row.original.name)" :key="dag.name">
                    <span v-if="i > 0" class="mr-1 text-xs text-dimmed">,</span>
                    <NuxtLink :to="dagLink(dag)" class="font-mono text-xs text-primary hover:underline">
                      {{ dag.name }}
                    </NuxtLink>
                  </template>
                </div>
                <span v-else class="text-muted">—</span>
              </template>

              <template #actions-cell="{ row }">
                <div class="flex justify-end">
                  <UButton
                    v-if="canManage && !row.original.orphaned"
                    size="sm"
                    icon="i-lucide-plus"
                    color="primary"
                    variant="soft"
                    label="Завести даг"
                    @click="openCreate(row.original)"
                  />
                </div>
              </template>

              <template #empty>
                <div class="py-6 text-center text-sm text-muted">
                  В образе нет дагов — проверьте, что бинарник объявляет их через
                  <span class="font-mono">loom.Main(...)</span>.
                </div>
              </template>
            </UTable>
          </UCard>

          <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
            <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
            <span>
              Состав задаёт код: один образ может нести несколько дагов. От каждого можно завести
              сколько угодно дагов — различаются они расписанием, пулом и своими переменными.
            </span>
          </p>
        </section>
      </template>

      <!-- заведение дага от шаблона -->
      <UModal
        v-model:open="createOpen"
        :title="`Новый даг от ${createTemplate?.name ?? ''}`"
        description="Настройки можно поменять позже в карточке дага."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField
              label="Имя дага"
              description="Уникально внутри проекта: полный идентификатор — «проект/даг»."
              :error="createNameTaken ? 'Даг с таким именем в проекте уже есть' : undefined"
            >
              <UInput v-model="createName" class="w-full font-mono" placeholder="nsi-sync-mechta" autofocus />
            </UFormField>
            <UFormField label="Пул слотов" description="Действует на все таски дага.">
              <USelectMenu v-model="createPool" :items="poolItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField label="Cron-расписание (опционально)" hint="пусто — запуск вручную">
              <UInput v-model="createSchedule" class="w-full font-mono" placeholder="0 3 * * *" />
            </UFormField>
            <template v-if="createSchedule.trim()">
              <UCheckbox
                v-model="createStartScheduled"
                label="Включить расписание сразу"
                description="Выключено — даг создаётся на паузе: расписание сохранится, но запуски только вручную до снятия паузы."
              />
              <UCheckbox
                v-model="createCatchup"
                label="Catchup"
                description="Наверстывать пропущенные тики расписания (ран на каждый тик, logical_date = тик)."
              />
            </template>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="createOpen = false" />
            <UButton
              label="Завести"
              :disabled="!canSubmitCreate"
              :loading="action.loading.value"
              @click="submitCreate"
            />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
