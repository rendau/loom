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

export interface DagTask {
  name: string
  depends_on: DagTaskDep[]
  retries: number
  retry_delay_sec: number
  timeout_sec: number
  resources?: DagTaskResources
  pool: string // пусто — default
  priority: number
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
}
