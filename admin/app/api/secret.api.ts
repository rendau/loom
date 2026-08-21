import { apiFetch } from '~/api/client'
import type { SecretMeta } from '~/types/secret'

export function listSecrets() {
  return apiFetch<{ results: SecretMeta[] }>('/secret')
}

// Создание секрета или перезапись значения существующего.
export function setSecret(name: string, value: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, { method: 'PUT', body: { value } })
}

export function deleteSecret(name: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, { method: 'DELETE' })
}
