// DTO дашборда (зеркало api/proto/server_v1/dashboard.proto). int64 в
// protojson приходят строками.

export interface DashboardWindow {
  success: string
  failed: string
}

export interface DashboardUpcoming {
  dag_name: string
  next_run_at: string
  schedule: string
}

export interface DashboardPool {
  name: string
  slots: string
  busy: string
}

export interface DashboardFailure {
  run_id: string
  dag_name: string
  finished_at: string
  // первый упавший таск и исход его последней попытки
  task: string
  exit_reason: string
}

export interface DashboardDay {
  date: string // YYYY-MM-DD (UTC)
  success: string
  failed: string
  running: string
}

export interface DashboardDagDuration {
  dag_name: string
  avg_sec: number
  max_sec: number
  runs: string
}

export interface Dashboard {
  active_runs: string
  dag_count: string
  paused_dag_count: string
  last_24h: DashboardWindow
  last_7d: DashboardWindow
  upcoming: DashboardUpcoming[]
  pools: DashboardPool[]
  recent_failures: DashboardFailure[]
  activity: DashboardDay[]
  dag_durations: DashboardDagDuration[]
}
