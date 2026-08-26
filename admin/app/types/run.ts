// DTO ранов (зеркало api/proto/server_v1/run.proto).

import type { Scope } from '~/types/common'

export type RunStatus = 'running' | 'success' | 'failed' | 'canceled'

export type TaskStatus =
  | 'pending'
  | 'queued'
  | 'starting'
  | 'running'
  | 'up_for_retry'
  | 'success'
  | 'failed'
  | 'upstream_failed'
  | 'canceled'

export type AttemptStatus = 'starting' | 'running' | 'success' | 'failed'

export interface Run {
  id: string
  project: string
  dag_name: string
  // Имя дага в образе, которым запускаются таски.
  template: string
  image: string
  image_digest: string
  trigger: string // manual | schedule | backfill
  status: RunStatus
  created_at: string
  finished_at?: string
  // «Дата данных»: тик расписания у cron/backfill-рана, момент триггера у ручного.
  logical_date: string
  // Параметры рана (аналог dagrun.conf); отсутствуют — без параметров.
  params?: Record<string, unknown>
}

// Запись env-снапшота рана: фактический резолв привязки при launch.
// Значение секрета не сохраняется — всегда пусто.
export interface RunEnv {
  env: string
  kind: 'variable' | 'secret'
  name: string
  // Источник значения: глобальный скоуп, проект или даг.
  scope: Scope
  value: string
  resolved_at: string
}

// Мелкое значение таска (аналог XCom).
export interface TaskValue {
  task: string
  key: string
  value: unknown
  modified_at: string
}

// Счётчики ранов по статусам (int64 → строки).
export interface RunCount {
  running: string
  success: string
  failed: string
  canceled: string
}

export interface TaskInstance {
  task: string
  status: TaskStatus
  attempt: number // 0 — ещё не стартовал
  queued_at?: string
  started_at?: string
  finished_at?: string
  retry_at?: string
}

export interface Attempt {
  task: string
  attempt: number
  status: AttemptStatus
  created_at: string
  started_at?: string
  finished_at?: string
  exit_code?: number
  exit_reason: string
  // Пик памяти по семплам executor'а; int64 в protojson — строка.
  peak_memory_bytes?: string
}
