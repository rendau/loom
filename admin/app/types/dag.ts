// DTO дагов (зеркало api/proto/server_v1/dag.proto).

export interface DagTaskDep {
  task: string
  streamed: boolean
}

export interface DagTaskResources {
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
}

export interface DagTaskEnvSecret {
  env: string
  secret: string
}

export interface DagTaskEnvVariable {
  env: string
  variable: string
}

export interface DagTask {
  name: string
  depends_on: DagTaskDep[]
  retries: number
  retry_delay_sec: number
  timeout_sec: number
  resources?: DagTaskResources
  pool: string // пусто — default
  priority: number
  secrets: DagTaskEnvSecret[]
  variables: DagTaskEnvVariable[]
}

export interface Dag {
  name: string
  image: string
  image_digest: string
  schedule: string
  paused: boolean
  sdk_version: string
  tasks: DagTask[]
  created_at: string
  modified_at?: string
  next_run_at?: string
  catchup: boolean
  max_active_runs: number // 0 — без лимита
  auto_update: boolean // poll-синк новой версии образа (решение №30)
}

export type DagRegistrationStatus = 'pending' | 'running' | 'success' | 'failed'

// Запись очереди асинхронных регистраций дагов.
export interface DagRegistration {
  id: string
  image: string
  source: 'manual' | 'auto' // auto — перерегистрация по digest (авто-обновление)
  status: DagRegistrationStatus
  error: string
  dag_name: string // у manual пусто до успешного describe
  schedule?: string
  catchup?: boolean
  paused?: boolean
  auto_update?: boolean
  created_at: string
  started_at?: string
  finished_at?: string
}
