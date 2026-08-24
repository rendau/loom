import { apiFetch } from '~/api/client'
import type { SecretMeta } from '~/types/secret'

// dagName undefined — все скоупы; '' — только глобальные; имя дага — его.
export function listSecrets(dagName?: string) {
  return apiFetch<{ results: SecretMeta[] }>('/secret', {
    query: dagName === undefined ? {} : { dag_name: dagName },
  })
}

// Создание секрета или перезапись значения существующего.
export function setSecret(dagName: string, name: string, value: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { value, dag_name: dagName },
  })
}

export function deleteSecret(dagName: string, name: string) {
  return apiFetch<object>(`/secret/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    query: { dag_name: dagName },
  })
}

// Значение секрета («посмотреть по кнопке»); доступ ограничен ролями.
export function getSecretValue(dagName: string, name: string) {
  return apiFetch<{ value: string }>(`/secret/${encodeURIComponent(name)}/value`, {
    query: { dag_name: dagName },
  })
}
