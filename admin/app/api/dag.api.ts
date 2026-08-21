import { apiFetch } from '~/api/client'
import type { ListParams, PaginationInfo } from '~/types/common'
import type { Dag } from '~/types/dag'

export type DagListQuery = {
  list_params: ListParams
  paused?: boolean
}

export function listDags(query: DagListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Dag[] }>('/dag', { query: { ...query } })
}

export function getDag(name: string) {
  return apiFetch<Dag>(`/dag/${encodeURIComponent(name)}`)
}

// Регистрация (или перерегистрация — новая версия образа) дага по url
// docker-образа; имя дага берётся из манифеста. auto_update undefined —
// сохранить текущее значение флага (перерегистрация его не сбрасывает).
export function registerDag(image: string, autoUpdate?: boolean) {
  return apiFetch<{ dag: Dag }>('/dag', { method: 'POST', body: { image, auto_update: autoUpdate } })
}

export function setDagPaused(name: string, paused: boolean) {
  return apiFetch<object>(`/dag/${encodeURIComponent(name)}/paused`, { method: 'PUT', body: { paused } })
}

export function setDagAutoUpdate(name: string, autoUpdate: boolean) {
  return apiFetch<object>(`/dag/${encodeURIComponent(name)}/auto_update`, { method: 'PUT', body: { auto_update: autoUpdate } })
}

export function deleteDag(name: string) {
  return apiFetch<object>(`/dag/${encodeURIComponent(name)}`, { method: 'DELETE' })
}
