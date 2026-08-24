import { apiFetch } from '~/api/client'
import type { Variable } from '~/types/variable'

// dagName undefined — все скоупы; '' — только глобальные; имя дага — его.
export function listVariables(dagName?: string) {
  return apiFetch<{ results: Variable[] }>('/variable', {
    query: dagName === undefined ? {} : { dag_name: dagName },
  })
}

// Создание переменной или перезапись значения существующей.
export function setVariable(dagName: string, name: string, value: string) {
  return apiFetch<object>(`/variable/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, dag_name: dagName },
  })
}

export function deleteVariable(dagName: string, name: string) {
  return apiFetch<object>(`/variable/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { dag_name: dagName },
  })
}
