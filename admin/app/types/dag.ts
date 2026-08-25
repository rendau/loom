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

// Оверрайд ресурсов таска из админки: значения манифеста — рекомендуемые,
// непустое поле оверрайда приоритетнее при запуске попытки.
export interface TaskResourcesOverride {
  task: string
  cpu_request: string
  cpu_limit: string
  memory_request: string
  memory_limit: string
  modified_at?: string
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
  priority: number
  secrets: DagTaskEnvSecret[]
  variables: DagTaskEnvVariable[]
}

// Статус одного из последних ранов дага (статус-стрип списка).
export interface DagLastRun {
  run_id: string
  status: string
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
  // Пул слотов дага (задаётся только в админке): действует на все его
  // таски. Пусто — таски уходят в общий пул default.
  pool: string
  // Последние раны (новые первыми, до 5).
  last_runs?: DagLastRun[]
}

// Агрегат по таску за последние N завершённых ранов дага.
export interface DagTaskStat {
  task: string
  runs: string
  avg_duration_sec: number
  max_duration_sec: number
  avg_peak_memory_bytes?: string
  max_peak_memory_bytes?: string
}

export interface DagStats {
  runs: string // сколько завершённых ранов попало в агрегат
  tasks?: DagTaskStat[]
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
  pool?: string
  created_at: string
  started_at?: string
  finished_at?: string
}
