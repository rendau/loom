import type { AttemptStatus, RunStatus, TaskStatus } from '~/types/run'
import type { DagRegistrationStatus } from '~/types/dag'
import type { TaskLogSource } from '~/types/log'

// Единый словарь статусов: цвет + подпись + иконка (рендер — компонент
// StatusBadge). Цвета — семантические токены Nuxt UI (badge/button color).
// Правило: цвет никогда не единственный сигнал — рядом иконка и подпись.

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

export function runStatusIcon(status: RunStatus): string {
  switch (status) {
    case 'running': return 'i-lucide-loader-circle'
    case 'success': return 'i-lucide-circle-check'
    case 'failed': return 'i-lucide-circle-x'
    case 'canceled': return 'i-lucide-circle-slash'
    default: return 'i-lucide-circle'
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
    // следствие чужого провала, не проблема сама по себе — приглушённый
    // нейтральный, чтобы не отвлекать от реально упавшего таска
    case 'upstream_failed': return 'neutral'
    case 'canceled': return 'neutral'
    default: return 'neutral'
  }
}

export function taskStatusIcon(status: TaskStatus): string {
  switch (status) {
    case 'pending': return 'i-lucide-circle-dashed'
    case 'queued': return 'i-lucide-clock'
    case 'starting':
    case 'running': return 'i-lucide-loader-circle'
    case 'up_for_retry': return 'i-lucide-timer'
    case 'success': return 'i-lucide-circle-check'
    case 'failed': return 'i-lucide-circle-x'
    case 'upstream_failed': return 'i-lucide-circle-off'
    case 'canceled': return 'i-lucide-circle-slash'
    default: return 'i-lucide-circle'
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

export function attemptStatusIcon(status: AttemptStatus): string {
  switch (status) {
    case 'starting':
    case 'running': return 'i-lucide-loader-circle'
    case 'success': return 'i-lucide-circle-check'
    case 'failed': return 'i-lucide-circle-x'
    default: return 'i-lucide-circle'
  }
}

// Статусы очереди регистраций дагов.

export function regStatusColor(status: DagRegistrationStatus): BadgeColor {
  switch (status) {
    case 'success': return 'success'
    case 'failed': return 'error'
    case 'running': return 'info'
    default: return 'neutral'
  }
}

export function regStatusLabel(status: DagRegistrationStatus): string {
  switch (status) {
    case 'pending': return 'в очереди'
    case 'running': return 'выполняется'
    case 'success': return 'успех'
    case 'failed': return 'провал'
    default: return status
  }
}

export function regStatusIcon(status: DagRegistrationStatus): string {
  switch (status) {
    case 'pending': return 'i-lucide-clock'
    case 'running': return 'i-lucide-loader-circle'
    case 'success': return 'i-lucide-circle-check'
    case 'failed': return 'i-lucide-circle-x'
    default: return 'i-lucide-circle'
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
