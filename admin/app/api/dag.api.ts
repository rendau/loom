import { apiFetch } from '~/api/client'
import type { DagRef, ListParams, PaginationInfo } from '~/types/common'
import type { Dag, DagStats, TaskResourcesOverride } from '~/types/dag'

export type DagListQuery = {
  list_params: ListParams
  paused?: boolean
  // Фильтры: даги проекта / заведённые от конкретного шаблона.
  project?: string
  template?: string
}

// Путь дага: идентификатор составной — проект и имя инстанса.
function dagPath(ref: DagRef, suffix = '') {
  return `/dag/${encodeURIComponent(ref.project)}/${encodeURIComponent(ref.name)}${suffix}`
}

export function listDags(query: DagListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Dag[] }>('/dag', { query: { ...query } })
}

export function getDag(ref: DagRef) {
  return apiFetch<Dag>(dagPath(ref))
}

export interface DagCreateBody {
  project: string
  // Имя дага в образе (шаблон), от которого заводится инстанс.
  template: string
  // Имя инстанса; пусто — совпадает с именем шаблона.
  name?: string
  // Желаемые настройки нового дага.
  schedule?: string
  catchup?: boolean
  paused?: boolean
  pool?: string
}

// Заведение дага-инстанса от шаблона проекта: от одного шаблона их может
// быть сколько угодно — различаются настройками и своими переменными.
export function createDag(body: DagCreateBody) {
  return apiFetch<Dag>('/dag', { method: 'POST', body: { ...body } })
}

// Агрегаты по таскам дага за последние lastRuns завершённых ранов
// («жирные таски»); lastRuns 0 — дефолт сервера (20).
export function getDagStats(ref: DagRef, lastRuns?: number) {
  return apiFetch<DagStats>(dagPath(ref, '/stats'), {
    query: { last_runs: lastRuns },
  })
}

// Расписание живёт только на control plane (манифест его не содержит);
// пустая строка schedule снимает расписание.
export function setDagSchedule(ref: DagRef, schedule: string, catchup: boolean) {
  return apiFetch<object>(dagPath(ref, '/schedule'), { method: 'PUT', body: { schedule, catchup } })
}

export function setDagPaused(ref: DagRef, paused: boolean) {
  return apiFetch<object>(dagPath(ref, '/paused'), { method: 'PUT', body: { paused } })
}

// Пул слотов дага: действует на все его таски (в коде дага пула нет).
// Пустая строка снимает пул; применяется со следующего рана.
export function setDagPool(ref: DagRef, pool: string) {
  return apiFetch<object>(dagPath(ref, '/pool'), { method: 'PUT', body: { pool } })
}

export function deleteDag(ref: DagRef) {
  return apiFetch<object>(dagPath(ref), { method: 'DELETE' })
}

// Оверрайды ресурсов тасков: значения манифеста — рекомендуемые, непустое
// поле оверрайда приоритетнее и применяется при запуске попытки
// (подхватывается ретраями без перерегистрации).
export function listTaskResources(ref: DagRef) {
  return apiFetch<{ results: TaskResourcesOverride[] }>(dagPath(ref, '/task-resources'))
}

// Все поля пустые — оверрайд удаляется.
export function setTaskResources(ref: DagRef, task: string, res: {
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
}) {
  return apiFetch<object>(
    dagPath(ref, `/task-resources/${encodeURIComponent(task)}`),
    { method: 'PUT', body: { ...res } },
  )
}

export function deleteTaskResources(ref: DagRef, task: string) {
  return apiFetch<object>(
    dagPath(ref, `/task-resources/${encodeURIComponent(task)}`),
    { method: 'DELETE' },
  )
}
