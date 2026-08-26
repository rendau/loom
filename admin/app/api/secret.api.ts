import { apiFetch } from '~/api/client'
import type { Scope } from '~/types/common'
import type { SecretMeta } from '~/types/secret'

// scope undefined — все скоупы; иначе — только указанный.
export function listSecrets(scope?: Scope) {
  return apiFetch<{ results: SecretMeta[] }>('/secret', {
    query: scope === undefined ? {} : { scope: { ...scope } },
  })
}

// Создание секрета или перезапись значения существующего.
export function setSecret(scope: Scope, name: string, value: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, scope },
  })
}

export function deleteSecret(scope: Scope, name: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { scope: { ...scope } },
  })
}

// Значение секрета («посмотреть по кнопке»); доступ ограничен ролями.
export function getSecretValue(scope: Scope, name: string) {
  return apiFetch<{ value: string }>(`/secret/${encodeURIComponent(name)}/value`, {
    query: { scope: { ...scope } },
  })
}
