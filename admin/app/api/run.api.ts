import { apiFetch } from '~/api/client'
import type { ListParams, PaginationInfo } from '~/types/common'
import type { Attempt, Run, TaskInstance } from '~/types/run'

export type RunListQuery = {
  list_params: ListParams
  dag_name?: string
  status?: string
}

export function listRuns(query: RunListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Run[] }>('/run', { query: { ...query } })
}

export function getRun(id: string) {
  return apiFetch<{ run: Run, tasks: TaskInstance[], attempts: Attempt[] }>(`/run/${encodeURIComponent(id)}`)
}

export function triggerRun(dagName: string) {
  return apiFetch<{ run_id: string }>('/run', { method: 'POST', body: { dag_name: dagName } })
}
