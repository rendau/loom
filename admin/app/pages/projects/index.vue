<script setup lang="ts">
import type { DropdownMenuItem, TableColumn, TableRow } from '@nuxt/ui'
import { apiErrorMessage } from '~/api/client'
import {
  deleteProject,
  listProjectRegistrations,
  listProjects,
  registerProject,
  setProjectAutoUpdate,
  syncProject,
} from '~/api/project.api'
import type { Project, ProjectRegistration } from '~/types/project'

// Проекты — это docker-образы: один образ несёт один или несколько дагов
// («шаблонов»), а от каждого шаблона заводят даги-инстансы со своими
// настройками. Здесь регистрируют образ, следят за обновлениями и видят
// очередь регистраций; сами даги — в разделе «Даги».

const { isAdmin, canManageProject, canSyncProject } = useAuth()

const PAGE_SIZE = 100

const projects = ref<Project[]>([])
const totalCount = ref(0)
const page = ref(1) // UPagination — 1-based, API — 0-based
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()
const toast = useToast()

async function load() {
  loading.value = true
  try {
    const rep = await listProjects({
      list_params: {
        page: page.value - 1,
        page_size: PAGE_SIZE,
        with_total_count: true,
        sort: ['name'],
      },
    })
    projects.value = rep.results ?? []
    totalCount.value = Number(rep.pagination_info.total_count)
    loadError.value = ''
  }
  catch (error) {
    // ошибка загрузки — inline alert (тост исчезает, а страница остаётся пустой)
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

watch(page, load)

// ── очередь регистраций: панель статусов + поллинг, пока есть активные ──

const registrations = ref<ProjectRegistration[]>([])
let regTimer: ReturnType<typeof setInterval> | undefined

const activeRegistrations = computed(() =>
  registrations.value.filter(r => r.status === 'pending' || r.status === 'running'))

// провалы за последние сутки (плашку можно закрыть) — общий composable:
// та же плашка есть в карточке проекта
const { failed: failedRegistrations, dismiss: dismissFailed } = useRegistrationFailures(registrations)

function isUpdating(project: Project): boolean {
  return activeRegistrations.value.some(r => r.project_name === project.name)
}

async function loadRegistrations() {
  registrations.value = (await listProjectRegistrations({ limit: 50 })).results ?? []
}

function ensureRegPolling() {
  if (regTimer)
    return
  regTimer = setInterval(async () => {
    const wasActive = new Set(activeRegistrations.value.map(r => r.id))
    await loadRegistrations()

    const nowActive = new Set(activeRegistrations.value.map(r => r.id))
    const finished = [...wasActive].filter(id => !nowActive.has(id))
    if (finished.length > 0)
      await load()

    // пачка доезжает разом — тосты по одному завалили бы экран: за тик
    // поллинга не больше двух, успехи и провалы одной сводкой
    const done = finished.map(id => registrations.value.find(r => r.id === id)).filter(r => !!r)
    const succeeded = done.filter(r => r.status === 'success')
    const errored = done.filter(r => r.status === 'failed')
    if (succeeded.length === 1) {
      const reg = succeeded[0]!
      const created = (reg.result ?? []).filter(d => d.created).length
      toast.add({
        title: `Проект ${reg.project_name} зарегистрирован`,
        description: created > 0 ? `Заведено дагов: ${created}` : undefined,
        color: 'success',
      })
    }
    else if (succeeded.length > 1) {
      toast.add({
        title: `Проектов зарегистрировано: ${succeeded.length}`,
        description: succeeded.map(r => r.project_name).join(', '),
        color: 'success',
      })
    }
    if (errored.length === 1)
      toast.add({ title: `Регистрация ${errored[0]!.image} не удалась`, description: errored[0]!.error, color: 'error' })
    else if (errored.length > 1) {
      toast.add({
        title: `Регистраций не удалось: ${errored.length}`,
        description: 'Причины — в панели над списком проектов.',
        color: 'error',
      })
    }

    if (activeRegistrations.value.length === 0)
      stopRegPolling()
  }, 3000)
}

function stopRegPolling() {
  if (regTimer) {
    clearInterval(regTimer)
    regTimer = undefined
  }
}

onMounted(async () => {
  await load()
  await loadRegistrations()
  if (activeRegistrations.value.length > 0)
    ensureRegPolling()
})

onUnmounted(stopRegPolling)

// ── регистрация проекта ────────────────────────────────

const registerOpen = ref(false)
const registerName = ref('')
const registerImage = ref('')
const registerAutoUpdate = ref(true)
const registerCreateDags = ref(true)
const registerBusy = ref(false)

// имя проекта по умолчанию — из образа: registry/host/nsi-sync:latest → nsi-sync
watch(registerImage, (image) => {
  if (registerName.value)
    return
  const repo = image.split('@')[0]!.split(':')[0]!
  const last = repo.split('/').pop() ?? ''
  if (/^[a-z0-9][\w.-]*$/i.test(last))
    registerName.value = last
})

const canSubmitRegister = computed(() => registerName.value.trim() !== '' && registerImage.value.trim() !== '')

async function submitRegister() {
  if (!canSubmitRegister.value)
    return

  registerBusy.value = true
  const ok = await action.run(
    () => registerProject({
      name: registerName.value.trim(),
      image: registerImage.value.trim(),
      auto_update: registerAutoUpdate.value,
      create_dags: registerCreateDags.value,
    }),
    { success: 'Регистрация поставлена в очередь' },
  )
  registerBusy.value = false
  if (ok === undefined)
    return

  registerOpen.value = false
  registerName.value = ''
  registerImage.value = ''
  registerAutoUpdate.value = true
  registerCreateDags.value = true
  await loadRegistrations()
  ensureRegPolling()
}

// ── действия над проектом ──────────────────────────────

async function toggleAutoUpdate(project: Project) {
  const ok = await action.run(
    () => setProjectAutoUpdate(project.name, !project.auto_update),
    { success: project.auto_update ? 'Авто-обновление выключено' : 'Авто-обновление включено' },
  )
  if (ok !== undefined)
    await load()
}

// принудительное обновление из registry: перерегистрация текущего образа в
// общей очереди — статус доезжает тем же поллингом
async function sync(project: Project) {
  const ok = await action.run(
    () => syncProject(project.name),
    { success: `Обновление проекта ${project.name} поставлено в очередь` },
  )
  if (ok !== undefined) {
    await loadRegistrations()
    ensureRegPolling()
  }
}

const deleteTarget = ref<Project | null>(null)

async function confirmDelete() {
  const project = deleteTarget.value
  if (!project)
    return
  const ok = await action.run(() => deleteProject(project.name), { success: 'Проект удалён' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

function menuItems(project: Project): DropdownMenuItem[][] {
  const main: DropdownMenuItem[] = []
  // sync доступен шире прочих действий проекта: и владельцу его дага
  if (canSyncProject(project.name)) {
    main.push({
      label: 'Обновить из registry',
      icon: 'i-lucide-cloud-download',
      disabled: isUpdating(project),
      onSelect: () => sync(project),
    })
  }
  if (canManageProject(project.name)) {
    main.push({
      label: project.auto_update ? 'Выключить авто-обновление' : 'Включить авто-обновление',
      icon: 'i-lucide-refresh-ccw-dot',
      onSelect: () => toggleAutoUpdate(project),
    })
  }
  main.push({
    label: 'Даги проекта',
    icon: 'i-lucide-workflow',
    onSelect: () => navigateTo(`/dags?project=${encodeURIComponent(project.name)}`),
  })

  const groups: DropdownMenuItem[][] = [main]
  if (isAdmin.value) {
    groups.push([{
      label: 'Удалить…',
      icon: 'i-lucide-trash-2',
      color: 'error',
      onSelect: () => { deleteTarget.value = project },
    }])
  }
  return groups
}

function openProject(_e: Event, row: TableRow<Project>) {
  navigateTo(`/projects/${encodeURIComponent(row.original.name)}`)
}

const columns: TableColumn<Project>[] = [
  { accessorKey: 'name', header: 'Проект' },
  { id: 'image', header: 'Образ' },
  { id: 'size', header: 'Размер' },
  { id: 'dags', header: 'Дагов' },
  { id: 'modified', header: 'Обновлён' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="projects">
    <template #header>
      <UDashboardNavbar title="Проекты">
        <template #right>
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить список"
            @click="load"
          />
          <UButton v-if="isAdmin" icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки проектов"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <!-- активные и недавно провалившиеся регистрации: пачка сворачивается
           в одну строку, иначе список проектов уезжает за экран -->
      <DagRegistrationQueue
        :active="activeRegistrations"
        :failed="failedRegistrations"
        @dismiss-failed="dismissFailed"
      />

      <UTable
        :data="projects"
        :columns="columns"
        :loading="loading"
        :ui="{ ...denseTableUi, tr: 'cursor-pointer' }"
        @select="openProject"
      >
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <span class="font-medium text-highlighted">{{ row.original.name }}</span>
            <UBadge v-if="isUpdating(row.original)" color="info" variant="subtle" size="sm">
              <UIcon name="i-lucide-loader-circle" class="animate-spin" />
              обновляется
            </UBadge>
            <UTooltip v-if="row.original.auto_update" text="Авто-обновление: digest тега отслеживается в registry">
              <UBadge color="info" variant="subtle" size="sm">auto</UBadge>
            </UTooltip>
          </div>
        </template>

        <template #image-cell="{ row }">
          <!-- digest в списке не нужен (он в карточке проекта): здесь важен
               сам ref образа, длинный — обрезается по ширине колонки -->
          <div class="min-w-0 max-w-100 truncate font-mono text-xs text-highlighted" :title="row.original.image">
            {{ row.original.image }}
          </div>
        </template>

        <template #size-cell="{ row }">
          <span v-if="Number(row.original.image_size_bytes)" class="text-xs tabular-nums text-muted">
            {{ formatBytes(row.original.image_size_bytes) }}
          </span>
          <span v-else class="text-muted">—</span>
        </template>

        <template #dags-cell="{ row }">
          {{ row.original.dag_count }}
        </template>

        <template #modified-cell="{ row }">
          <RelativeTime :time="row.original.modified_at ?? row.original.created_at" />
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <RowMenu :items="menuItems(row.original)" />
          </div>
        </template>

        <template #empty>
          <!-- при ошибке загрузки пустота — не «проектов нет», причина в алерте выше -->
          <div v-if="loadError" class="py-6" />
          <EmptyState
            v-else
            icon="i-lucide-package"
            title="Проектов пока нет"
            description="Зарегистрируйте docker-образ: его даги появятся сразу после describe, а от каждого можно завести сколько угодно дагов."
          >
            <UButton v-if="isAdmin" size="sm" icon="i-lucide-plus" label="Зарегистрировать" @click="registerOpen = true" />
          </EmptyState>
        </template>
      </UTable>

      <div v-if="totalCount > PAGE_SIZE" class="flex justify-end border-t border-default p-2">
        <UPagination v-model:page="page" :total="totalCount" :items-per-page="PAGE_SIZE" size="sm" />
      </div>

      <!-- регистрация проекта -->
      <UModal
        v-model:open="registerOpen"
        title="Регистрация проекта"
        description="Pull образа и describe выполняются в фоне — статус появится в панели над таблицей."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField label="Docker-образ" hint="тег или digest">
              <UInput
                v-model="registerImage"
                class="w-full font-mono"
                placeholder="registry/my-project:latest"
                autofocus
              />
            </UFormField>
            <UFormField
              label="Имя проекта"
              description="Дальше не меняется: входит в идентификатор каждого дага проекта."
            >
              <UInput v-model="registerName" class="w-full font-mono" placeholder="my-project" />
            </UFormField>
            <UCheckbox
              v-model="registerCreateDags"
              label="Завести даги по дагам образа"
              description="На каждый даг образа создаётся один даг-инстанс с таким же именем. Выключено — шаблоны появятся, а даги заведёте вручную."
            />
            <UCheckbox
              v-model="registerAutoUpdate"
              label="Авто-обновление образа"
              description="Server будет следить за digest'ом тега в registry и перерегистрировать проект при изменении."
            />
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="registerOpen = false" />
            <UButton
              label="Зарегистрировать"
              :disabled="!canSubmitRegister"
              :loading="registerBusy"
              @click="submitRegister"
            />
          </div>
        </template>
      </UModal>

      <UModal
        :open="deleteTarget !== null"
        title="Удалить проект?"
        @update:open="deleteTarget = null"
      >
        <template #body>
          <p>
            Проект <span class="font-mono font-medium">{{ deleteTarget?.name }}</span> будет удалён
            вместе со всеми его дагами ({{ deleteTarget?.dag_count ?? 0 }}) и их настройками.
            История ранов останется.
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
