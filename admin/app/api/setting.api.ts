import { apiFetch } from '~/api/client'
import type { Setting } from '~/types/setting'

// dagName undefined — все скоупы; '' — только глобальные; имя дага — его.
export function listSettings(dagName?: string) {
  return apiFetch<{ results: Setting[] }>('/setting', {
    query: dagName === undefined ? {} : { dag_name: dagName },
  })
}

export function setSetting(dagName: string, name: string, value: string) {
  return apiFetch<object>(`/setting/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, dag_name: dagName },
  })
}

// Удаление уточнения настройки у дага (возврат к глобальному значению);
// глобальные значения не удаляются.
export function deleteSetting(dagName: string, name: string) {
  return apiFetch<object>(`/setting/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { dag_name: dagName },
  })
}
