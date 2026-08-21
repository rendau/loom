import { apiFetch } from '~/api/client'
import type { ListParams, PaginationInfo } from '~/types/common'
import type { DagTask } from '~/types/dag'
import type { Attempt, Run, TaskInstance, TaskValue } from '~/types/run'

export type RunListQuery = {
  list_params: ListParams
  dag_name?: string
  status?: string
}

export function listRuns(query: RunListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Run[] }>('/run', { query: { ...query } })
}

export function getRun(id: string) {
  // manifest_tasks — таски из снапшота манифеста рана (рёбра графа);
  // gateway опускает пустые repeated-поля, поэтому optional.
  return apiFetch<{ run: Run, tasks: TaskInstance[], attempts: Attempt[], manifest_tasks?: DagTask[] }>(`/run/${encodeURIComponent(id)}`)
}

export function triggerRun(dagName: string, params?: Record<string, unknown>) {
  return apiFetch<{ run_id: string }>('/run', {
    method: 'POST',
    body: { dag_name: dagName, ...(params ? { params } : {}) },
  })
}

// Backfill: по рану на каждый тик cron-расписания в [from, to) —
// ISO-таймстампы; params — общие параметры всех создаваемых ранов.
export function backfillRuns(dagName: string, from: string, to: string, params?: Record<string, unknown>) {
  return apiFetch<{ run_ids: string[] }>('/run/backfill', {
    method: 'POST',
    body: { dag_name: dagName, from, to, ...(params ? { params } : {}) },
  })
}

export function listRunValues(runId: string) {
  return apiFetch<{ values: TaskValue[] }>(`/run/${encodeURIComponent(runId)}/value`)
}

// Ретрай таска завершённого рана: таск уходит в очередь новой попыткой,
// его downstream-подграф сбрасывается и выполняется заново.
export function retryTask(runId: string, task: string) {
  return apiFetch<object>(
    `/run/${encodeURIComponent(runId)}/task/${encodeURIComponent(task)}/retry`,
    { method: 'POST', body: {} },
  )
}
