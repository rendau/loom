// DTO ранов (зеркало api/proto/server_v1/run.proto).

export type RunStatus = 'running' | 'success' | 'failed'

export type TaskStatus =
  | 'pending'
  | 'queued'
  | 'starting'
  | 'running'
  | 'up_for_retry'
  | 'success'
  | 'failed'
  | 'upstream_failed'

export type AttemptStatus = 'starting' | 'running' | 'success' | 'failed'

export interface Run {
  id: string
  dag_name: string
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

// Мелкое значение таска (аналог XCom).
export interface TaskValue {
  task: string
  key: string
  value: unknown
  modified_at: string
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
