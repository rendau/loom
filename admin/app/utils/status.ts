import type { AttemptStatus, RunStatus, TaskStatus } from '~/types/run'
import type { TaskLogSource } from '~/types/log'

// Цвета и подписи статусов. Цвета — семантические токены Nuxt UI
// (badge/button color).

type BadgeColor = 'neutral' | 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'error'

export function runStatusColor(status: RunStatus): BadgeColor {
  switch (status) {
    case 'running': return 'info'
    case 'success': return 'success'
    case 'failed': return 'error'
    case 'canceled': return 'neutral'
    default: return 'neutral'
  }
}

export function runStatusLabel(status: RunStatus): string {
  switch (status) {
    case 'running': return 'выполняется'
    case 'success': return 'успех'
    case 'failed': return 'провал'
    case 'canceled': return 'остановлен'
    default: return status
  }
}

export function runTriggerLabel(trigger: string): string {
  switch (trigger) {
    case 'manual': return 'вручную'
    case 'schedule': return 'расписание'
    case 'backfill': return 'backfill'
    default: return trigger
  }
}

export function runTriggerColor(trigger: string): BadgeColor {
  switch (trigger) {
    case 'schedule': return 'secondary'
    case 'backfill': return 'info'
    default: return 'neutral'
  }
}

export function taskStatusColor(status: TaskStatus): BadgeColor {
  switch (status) {
    case 'pending': return 'neutral'
    case 'queued': return 'secondary'
    case 'starting': return 'info'
    case 'running': return 'info'
    case 'up_for_retry': return 'warning'
    case 'success': return 'success'
    case 'failed': return 'error'
    case 'upstream_failed': return 'warning'
    case 'canceled': return 'neutral'
    default: return 'neutral'
  }
}

export function taskStatusLabel(status: TaskStatus): string {
  switch (status) {
    case 'pending': return 'ожидает зависимости'
    case 'queued': return 'в очереди'
    case 'starting': return 'запускается'
    case 'running': return 'выполняется'
    case 'up_for_retry': return 'ждёт ретрая'
    case 'success': return 'успех'
    case 'failed': return 'провал'
    case 'upstream_failed': return 'провал зависимости'
    case 'canceled': return 'остановлен'
    default: return status
  }
}

export function attemptStatusColor(status: AttemptStatus): BadgeColor {
  switch (status) {
    case 'starting': return 'info'
    case 'running': return 'info'
    case 'success': return 'success'
    case 'failed': return 'error'
    default: return 'neutral'
  }
}

// Цвет строки лога по источнику: stderr и строки control plane выделяем.
export function logSourceClass(source: TaskLogSource): string {
  switch (source) {
    case 'TASK_LOG_SOURCE_STDERR': return 'text-warning'
    case 'TASK_LOG_SOURCE_SERVER': return 'text-info'
    default: return 'text-muted'
  }
}

export function logSourceLabel(source: TaskLogSource): string {
  switch (source) {
    case 'TASK_LOG_SOURCE_LOG': return 'log'
    case 'TASK_LOG_SOURCE_STDOUT': return 'out'
    case 'TASK_LOG_SOURCE_STDERR': return 'err'
    case 'TASK_LOG_SOURCE_SERVER': return 'srv'
    default: return '?'
  }
}
