import { apiFetch } from '~/api/client'
import type { Dashboard } from '~/types/dashboard'

export function getDashboard() {
  return apiFetch<Dashboard>('/dashboard')
}
