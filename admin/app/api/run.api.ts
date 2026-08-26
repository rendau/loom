import { apiFetch } from '~/api/client'
import type { DagRef, ListParams, PaginationInfo } from '~/types/common'
import type { DagTask } from '~/types/dag'
import type { Attempt, Run, RunCount, RunEnv, TaskInstance, TaskValue } from '~/types/run'

export type RunListQuery = {
  list_params: ListParams
  // Фильтры: конкретный даг (project + dag_name) или все раны проекта.
  project?: string
  dag_name?: string
  status?: string
}

// Фильтр списка ранов по дагу: пара задаётся целиком.
export function runDagQuery(ref?: DagRef) {
  return ref ? { project: ref.project, dag_name: ref.name } : {}
}

export function listRuns(query: RunListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Run[] }>('/run', { query: { ...query } })
}

export function getRun(id: string) {
  // manifest_tasks — таски из снапшота манифеста рана (рёбра графа);
  // gateway опускает пустые repeated-поля, поэтому optional.
  return apiFetch<{
    run: Run
    tasks: TaskInstance[]
    attempts: Attempt[]
    manifest_tasks?: DagTask[]
    // снапшот env-резолва рана (пуст у ранов до введения run_env)
    env?: RunEnv[]
  }>(`/run/${encodeURIComponent(id)}`)
}

export function triggerRun(ref: DagRef, params?: Record<string, unknown>) {
  return apiFetch<{ run_id: string }>('/run', {
    method: 'POST',
    body: { project: ref.project, dag_name: ref.name, ...(params ? { params } : {}) },
  })
}

// Backfill: по рану на каждый тик cron-расписания в [from, to) —
// ISO-таймстампы; params — общие параметры всех создаваемых ранов.
export function backfillRuns(ref: DagRef, from: string, to: string, params?: Record<string, unknown>) {
  return apiFetch<{ run_ids: string[] }>('/run/backfill', {
    method: 'POST',
    body: { project: ref.project, dag_name: ref.name, from, to, ...(params ? { params } : {}) },
  })
}

export function listRunValues(runId: string) {
  return apiFetch<{ values: TaskValue[] }>(`/run/${encodeURIComponent(runId)}/value`)
}

// Счётчики ранов по статусам — для фильтров-чипов списка.
export function countRuns(ref?: DagRef) {
  return apiFetch<RunCount>('/run-count', { query: runDagQuery(ref) })
}

// Принудительная остановка выполняющегося рана: живые попытки убиваются,
// незавершённые таски и сам ран получают статус canceled.
export function cancelRun(runId: string) {
  return apiFetch<object>(`/run/${encodeURIComponent(runId)}/cancel`, { method: 'POST', body: {} })
}

// Ретрай таска завершённого рана: таск уходит в очередь новой попыткой,
// его downstream-подграф сбрасывается и выполняется заново.
export function retryTask(runId: string, task: string) {
  return apiFetch<object>(
    `/run/${encodeURIComponent(runId)}/task/${encodeURIComponent(task)}/retry`,
    { method: 'POST', body: {} },
  )
}
