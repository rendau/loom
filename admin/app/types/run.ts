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
  trigger: string // manual | schedule
  status: RunStatus
  created_at: string
  finished_at?: string
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
}
