// DTO логов попыток (зеркало api/proto/server_v1/task_log.proto).

export type TaskLogSource =
  | 'TASK_LOG_SOURCE_UNSPECIFIED'
  | 'TASK_LOG_SOURCE_LOG'
  | 'TASK_LOG_SOURCE_STDOUT'
  | 'TASK_LOG_SOURCE_STDERR'
  | 'TASK_LOG_SOURCE_SERVER'

export interface TaskLogEntry {
  ts_unix_ms: string // int64 → строка
  source: TaskLogSource
  line: string
}
