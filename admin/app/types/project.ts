// DTO проектов (зеркало api/proto/server_v1/project.proto).
//
// Проект — это docker-образ: он несёт один или несколько дагов
// («шаблонов»), а от каждого шаблона заводят даги-инстансы со своими
// настройками.

import type { DagTask } from '~/types/dag'

// Даг, объявленный в образе. Манифест хранится один раз здесь, а не
// копией у каждого инстанса.
export interface ProjectTemplate {
  name: string
  sdk_version: string
  // Шаблон пропал из образа при последней регистрации: его инстансы
  // работают на последнем известном манифесте.
  orphaned: boolean
  max_active_runs: number // 0 — без лимита
  tasks: DagTask[]
  dag_count: number // сколько инстансов заведено от шаблона
  created_at: string
  modified_at?: string
}

export interface Project {
  name: string
  image: string
  image_digest: string
  // Размер образа в registry (config + слои): int64 → строка в protojson.
  // '0' — registry не удалось опросить при регистрации.
  image_size_bytes: string
  auto_update: boolean // poll-синк новой версии образа
  created_at: string
  modified_at?: string
  // Каталог образа: заполняется в getProject, в списке пуст.
  templates?: ProjectTemplate[]
  dag_count: number
}

export type ProjectRegistrationStatus = 'pending' | 'running' | 'success' | 'failed'

// Судьба одного дага образа при регистрации.
export interface ProjectRegistrationDag {
  name: string
  error?: string // пусто — шаблон зарегистрирован
  created?: boolean // заведён новый даг-инстанс
}

// Запись очереди асинхронных регистраций проектов.
export interface ProjectRegistration {
  id: string
  project_name: string
  image: string
  source: 'manual' | 'auto' // auto — перерегистрация по digest (авто-обновление)
  status: ProjectRegistrationStatus
  error: string
  auto_update?: boolean
  create_dags: boolean
  result?: ProjectRegistrationDag[]
  created_at: string
  started_at?: string
  finished_at?: string
}
