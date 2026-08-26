import { apiFetch } from '~/api/client'
import type { ListParams, PaginationInfo } from '~/types/common'
import type { Project, ProjectRegistration } from '~/types/project'

export type ProjectListQuery = {
  list_params: ListParams
  auto_update?: boolean
}

export function listProjects(query: ProjectListQuery) {
  return apiFetch<{ pagination_info: PaginationInfo, results: Project[] }>('/project', { query: { ...query } })
}

// Проект вместе с каталогом образа (шаблонами).
export function getProject(name: string) {
  return apiFetch<Project>(`/project/${encodeURIComponent(name)}`)
}

export interface ProjectRegisterBody {
  // Имя проекта: задаётся при регистрации, дальше не меняется.
  name: string
  image: string
  // auto_update undefined — сохранить текущее значение флага
  // (перерегистрация его не сбрасывает).
  auto_update?: boolean
  // Заводить даги-инстансы по новым дагам образа (по умолчанию true).
  create_dags?: boolean
}

// Регистрация асинхронная: ответ — id записи очереди, статус поллится
// через listProjectRegistrations/getProjectRegistration.
export function registerProject(body: ProjectRegisterBody) {
  return apiFetch<{ registration_id: string }>('/project', { method: 'POST', body: { ...body } })
}

// Принудительное обновление проекта из registry: перерегистрация текущего
// образа сейчас, не дожидаясь тика авто-обновления. Обновляет шаблоны всех
// дагов образа разом; новые даги-инстансы при этом не заводятся.
export function syncProject(name: string) {
  return apiFetch<{ registration_id: string }>(
    `/project/${encodeURIComponent(name)}/sync`,
    { method: 'POST', body: {} },
  )
}

export function setProjectAutoUpdate(name: string, autoUpdate: boolean) {
  return apiFetch<object>(
    `/project/${encodeURIComponent(name)}/auto_update`,
    { method: 'PUT', body: { auto_update: autoUpdate } },
  )
}

// Удаляет проект вместе с шаблонами и всеми его дагами.
export function deleteProject(name: string) {
  return apiFetch<object>(`/project/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

export function listProjectRegistrations(
  query: { project_name?: string, active?: boolean, limit?: number } = {},
) {
  return apiFetch<{ results: ProjectRegistration[] }>('/project-registration', { query: { ...query } })
}

export function getProjectRegistration(id: string) {
  return apiFetch<ProjectRegistration>(`/project-registration/${encodeURIComponent(id)}`)
}
