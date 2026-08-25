import { apiFetch } from '~/api/client'
import type { ListParams, PaginationInfo } from '~/types/common'
import type { Dag, DagRegistration, DagStats } from '~/types/dag'

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

export interface DagRegisterBody {
  image: string
  // auto_update undefined — сохранить текущее значение флага
  // (перерегистрация его не сбрасывает).
  auto_update?: boolean
  // Желаемые настройки нового дага; к существующему не применяются.
  schedule?: string
  catchup?: boolean
  paused?: boolean
}

// Регистрация асинхронная: ответ — id записи очереди, статус поллится
// через listDagRegistrations/getDagRegistration.
export function registerDag(body: DagRegisterBody) {
  return apiFetch<{ registration_id: string }>('/dag', { method: 'POST', body: { ...body } })
}

export function listDagRegistrations(query: { dag_name?: string, active?: boolean, limit?: number } = {}) {
  return apiFetch<{ results: DagRegistration[] }>('/dag-registration', { query: { ...query } })
}

// Агрегаты по таскам дага за последние lastRuns завершённых ранов
// («жирные таски»); lastRuns 0 — дефолт сервера (20).
export function getDagStats(name: string, lastRuns?: number) {
  return apiFetch<DagStats>(`/dag/${encodeURIComponent(name)}/stats`, {
    query: { last_runs: lastRuns },
  })
}

export function getDagRegistration(id: string) {
  return apiFetch<DagRegistration>(`/dag-registration/${encodeURIComponent(id)}`)
}

// Принудительное обновление дага из registry: перерегистрация его текущего
// образа сейчас, не дожидаясь тика авто-обновления. Ответ — id записи
// очереди регистраций (статус поллится там же, где у registerDag).
export function syncDag(name: string) {
  return apiFetch<{ registration_id: string }>(
    `/dag/${encodeURIComponent(name)}/sync`,
    { method: 'POST', body: {} },
  )
}

// Расписание живёт только на control plane (манифест его не содержит);
// пустая строка schedule снимает расписание.
export function setDagSchedule(name: string, schedule: string, catchup: boolean) {
  return apiFetch<object>(`/dag/${encodeURIComponent(name)}/schedule`, { method: 'PUT', body: { schedule, catchup } })
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
