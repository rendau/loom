import { apiFetch } from '~/api/client'
import type { Pool } from '~/types/pool'

export function listPools() {
  return apiFetch<{ results: Pool[] }>('/pool')
}

// Создание пула или изменение слотов существующего; удаления нет.
export function setPool(name: string, slots: number) {
  return apiFetch<object>(`/pool/${encodeURIComponent(name)}`, { method: 'PUT', body: { slots } })
}
