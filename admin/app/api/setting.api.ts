import { apiFetch } from '~/api/client'
import type { Scope } from '~/types/common'
import type { Setting } from '~/types/setting'

// scope undefined — все скоупы; иначе — только указанный.
export function listSettings(scope?: Scope) {
  return apiFetch<{ results: Setting[] }>('/setting', {
    query: scope === undefined ? {} : { scope: { ...scope } },
  })
}

export function setSetting(scope: Scope, name: string, value: string) {
  return apiFetch<object>(`/setting/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, scope },
  })
}

// Удаление уточнения настройки у проекта или дага (возврат к более
// широкому скоупу); глобальные значения не удаляются.
export function deleteSetting(scope: Scope, name: string) {
  return apiFetch<object>(`/setting/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { scope: { ...scope } },
  })
}
