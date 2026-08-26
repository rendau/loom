<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import { getProject } from '~/api/project.api'
import { listSecrets } from '~/api/secret.api'
import { listVariables } from '~/api/variable.api'
import type { Dag } from '~/types/dag'
import type { Project, ProjectTemplate } from '~/types/project'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Страница шаблона — как устроен даг в образе: граф, таски и что он
// требует от окружения. Это свойства КОДА, общие для всех инстансов;
// поведение (раны, расписание, значения переменных) смотрят в карточке
// каждого инстанса.
//
// Отдельного RPC у шаблона нет: манифест приезжает каталогом в GetProject.

const route = useRoute()
const projectName = String(route.params.name)
const templateName = String(route.params.template)

const { canManageProject } = useAuth()
const canManage = computed(() => canManageProject(projectName))

const project = ref<Project | null>(null)
const dags = ref<Dag[]>([])
const variables = ref<Variable[]>([])
const secrets = ref<SecretMeta[]>([])
const loading = ref(false)
const loadError = ref('')

const crumbs = [
  { label: 'Проекты', icon: 'i-lucide-package', to: '/projects' },
  { label: projectName, to: `/projects/${encodeURIComponent(projectName)}` },
  { label: templateName },
]

const template = computed<ProjectTemplate | null>(() =>
  project.value?.templates?.find(t => t.name === templateName) ?? null)

// инстансы, заведённые от этого шаблона
const instances = computed(() => dags.value.filter(d => d.template === templateName))

const tasks = computed(() => template.value?.tasks ?? [])

async function load() {
  loading.value = true
  try {
    const [p, d] = await Promise.all([
      getProject(projectName),
      listDags({ list_params: { page_size: 200, sort: ['name'] }, project: projectName }),
    ])
    project.value = p
    dags.value = d.results ?? []
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

// значения переменных и секретов — для колонки «заполнено»; best effort:
// без них страница всё равно показывает состав требований
async function loadEnv() {
  try {
    const [v, s] = await Promise.all([listVariables(), listSecrets()])
    variables.value = v.results ?? []
    secrets.value = s.results ?? []
  }
  catch {
    variables.value = []
    secrets.value = []
  }
}

onMounted(async () => {
  await Promise.all([load(), loadEnv()])
})

// ── что шаблон требует от окружения ─────────────────────
//
// Своего скоупа у шаблона нет: значение либо заведено на проект/глобально
// (тогда его получат все инстансы), либо каждый инстанс определяет своё.
// Поэтому резолвим по скоупу проекта, а рядом считаем, у скольких
// инстансов значение фактически есть.

const requirements = computed(() =>
  resolveDagEnv(tasks.value, { project: projectName, name: '' }, variables.value, secrets.value))

// сколько инстансов имеют значение — резолв тот же, что при launch
const filledInstances = computed(() => {
  const out = new Map<string, number>()
  for (const req of requirements.value) {
    const filled = instances.value.filter((d) => {
      const resolved = resolveDagEnv(tasks.value, d, variables.value, secrets.value)
      return resolved.find(r => r.kind === req.kind && r.name === req.name)?.scope !== undefined
    }).length
    out.set(`${req.kind}:${req.name}`, filled)
  }
  return out
})

function filledCount(kind: string, name: string): number {
  return filledInstances.value.get(`${kind}:${name}`) ?? 0
}
</script>

<template>
  <UDashboardPanel id="dag-template">
    <template #header>
      <UDashboardNavbar :title="templateName">
        <template #leading>
          <UButton
            icon="i-lucide-arrow-left"
            color="neutral"
            variant="ghost"
            :to="`/projects/${encodeURIComponent(projectName)}`"
            aria-label="К проекту"
          />
        </template>
        <template #title>
          <PageCrumbs :items="crumbs" kind="даг в образе" />
        </template>
        <template #right>
          <UBadge v-if="template?.orphaned" color="warning" variant="subtle" size="lg">
            исчез из образа
          </UBadge>
          <UButton
            icon="i-lucide-refresh-cw"
            color="neutral"
            variant="ghost"
            :loading="loading"
            aria-label="Обновить"
            @click="load"
          />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки шаблона"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <EmptyState
        v-else-if="!loading && !template"
        icon="i-lucide-file-question"
        :title="`Дага ${templateName} нет в образе`"
        description="Шаблон мог исчезнуть из проекта: посмотрите каталог образа в карточке проекта."
      >
        <UButton size="sm" label="К проекту" :to="`/projects/${encodeURIComponent(projectName)}`" />
      </EmptyState>

      <template v-if="template">
        <MetaGrid>
          <MetaItem label="Образ" span>
            <CopyText :text="project?.image ?? ''" mono />
          </MetaItem>
          <MetaItem label="SDK">
            <span class="font-mono">{{ template.sdk_version || '—' }}</span>
          </MetaItem>
          <MetaItem label="Тасков">{{ tasks.length }}</MetaItem>
          <MetaItem label="Лимит активных ранов">{{ template.max_active_runs || 'без лимита' }}</MetaItem>
          <MetaItem label="Заведено дагов">
            <!-- инстансы различаются настройками и переменными, код у них общий -->
            <div v-if="instances.length" class="flex flex-wrap items-baseline">
              <template v-for="(dag, i) in instances" :key="dag.name">
                <span v-if="i > 0" class="mr-1 text-dimmed">,</span>
                <NuxtLink :to="dagLink(dag)" class="font-mono text-primary hover:underline">
                  {{ dag.name }}
                </NuxtLink>
              </template>
            </div>
            <span v-else class="text-muted">— (ни одного)</span>
          </MetaItem>
        </MetaGrid>

        <UAlert
          v-if="template.orphaned"
          color="warning"
          variant="subtle"
          icon="i-lucide-triangle-alert"
          title="Дага нет в последней версии образа"
          description="Заведённые инстансы продолжают работать на последнем известном манифесте — схема ниже показывает именно его."
        />

        <section>
          <SectionHeader title="Граф" />
          <RunDagGraph :manifest-tasks="tasks" />
        </section>

        <section>
          <SectionHeader title="Таски" :count="tasks.length" />
          <!-- оверрайды ресурсов задаются на даге-инстансе, у шаблона их
               нет: здесь видно то, что объявил код -->
          <DagTaskTable :tasks="tasks" />
        </section>

        <section>
          <SectionHeader title="Требует от окружения" :count="requirements.length" />
          <UCard :ui="{ body: 'p-0 sm:p-0' }">
            <div v-if="requirements.length" class="divide-y divide-default">
              <div v-for="req in requirements" :key="`${req.kind}:${req.name}`" class="flex items-baseline gap-2 p-3">
                <UIcon
                  :name="req.kind === 'secret' ? 'i-lucide-key-round' : 'i-lucide-variable'"
                  class="size-4 shrink-0 self-center"
                  :class="req.kind === 'secret' ? 'text-warning' : 'text-muted'"
                />
                <div class="min-w-0 flex-1">
                  <div class="flex flex-wrap items-baseline gap-2">
                    <span class="font-mono font-medium text-highlighted">{{ req.name }}</span>
                    <span class="font-mono text-xs text-dimmed">env: {{ req.envs.join(', ') }}</span>
                    <span class="text-xs text-muted">таски: {{ req.tasks.join(', ') }}</span>
                  </div>
                  <p v-if="req.description" class="mt-0.5 text-xs text-muted">{{ req.description }}</p>
                </div>
                <div class="shrink-0 text-xs">
                  <!-- значение на проекте/глобально достаётся всем
                       инстансам сразу; иначе его заводят каждому свой -->
                  <UBadge v-if="req.scope" color="success" variant="subtle" size="sm">
                    {{ scopeLabel(req.scope) || 'глобально' }}
                  </UBadge>
                  <UTooltip v-else :text="`Значение резолвится у каждого инстанса отдельно (${filledCount(req.kind, req.name)} из ${instances.length})`">
                    <UBadge
                      :color="instances.length > 0 && filledCount(req.kind, req.name) === instances.length ? 'neutral' : 'error'"
                      variant="subtle"
                      size="sm"
                    >
                      заполнено у {{ filledCount(req.kind, req.name) }} из {{ instances.length }}
                    </UBadge>
                  </UTooltip>
                </div>
              </div>
            </div>
            <p v-else class="p-3 text-sm text-muted">
              Даг не объявляет переменных и секретов.
            </p>
          </UCard>
          <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
            <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
            <span>
              Состав задаёт код дага. Значение можно завести один раз на проект (получат все инстансы)
              или отдельно каждому инстансу — например, свой магазин у каждого.
              Заполняют на странице
              <NuxtLink :to="`/env?scope=${encodeURIComponent(projectName)}`" class="text-primary hover:underline">
                переменных и секретов
              </NuxtLink>
              или в табе «Env» инстанса.
            </span>
          </p>
        </section>

        <p v-if="canManage" class="text-xs text-muted">
          Новый даг от этого шаблона заводится в
          <NuxtLink :to="`/projects/${encodeURIComponent(projectName)}`" class="text-primary hover:underline">карточке проекта</NuxtLink>.
        </p>
      </template>
    </template>
  </UDashboardPanel>
</template>
