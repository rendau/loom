import { apiFetch } from '~/api/client'
import type { Scope } from '~/types/common'
import type { Variable } from '~/types/variable'

// scope undefined — все скоупы; иначе — только указанный.
export function listVariables(scope?: Scope) {
  return apiFetch<{ results: Variable[] }>('/variable', {
    query: scope === undefined ? {} : { scope: { ...scope } },
  })
}

// Создание переменной или перезапись значения существующей.
export function setVariable(scope: Scope, name: string, value: string) {
  return apiFetch<object>(`/variable/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, scope },
  })
}

// Перенос переменной в другой скоуп: значение переезжает как есть, права
// нужны на оба скоупа.
export function moveVariable(from: Scope, to: Scope, name: string) {
  return apiFetch<object>(`/variable/${encodeURIComponent(name)}/move`, {
    method: 'POST',
    body: { scope: from, to_scope: to },
  })
}

export function deleteVariable(scope: Scope, name: string) {
  return apiFetch<object>(`/variable/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { scope: { ...scope } },
  })
}
