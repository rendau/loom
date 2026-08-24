import { apiFetch } from '~/api/client'
import type { LoginRep, User, UserRole } from '~/types/user'

export function getAuthStatus() {
  return apiFetch<{ users_exist: boolean }>('/auth/status')
}

// Первичная настройка: создать администратора и сразу войти.
export function createFirstAdmin(username: string, password: string) {
  return apiFetch<LoginRep>('/auth/first-admin', { method: 'POST', body: { username, password } })
}

export function login(username: string, password: string) {
  return apiFetch<LoginRep>('/auth/login', { method: 'POST', body: { username, password } })
}

export function logout() {
  return apiFetch<object>('/auth/logout', { method: 'POST', body: {} })
}

export function getMe() {
  return apiFetch<User>('/auth/me')
}

// ── управление пользователями (только admin) ────────────

export function listUsers() {
  return apiFetch<{ results: User[] }>('/user')
}

export function createUser(body: { username: string, password: string, role: UserRole, dag_names: string[] }) {
  return apiFetch<User>('/user', { method: 'POST', body: { ...body } })
}

export interface UserUpdateBody {
  password?: string
  role?: UserRole
  dag_names?: string[]
  // true — заменить набор назначенных дагов (в т.ч. очистить).
  set_dag_names?: boolean
}

export function updateUser(id: string, body: UserUpdateBody) {
  return apiFetch<object>(`/user/${encodeURIComponent(id)}`, { method: 'PUT', body: { ...body } })
}

export function deleteUser(id: string) {
  return apiFetch<object>(`/user/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
