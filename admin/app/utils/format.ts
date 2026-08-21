// Показ дат и длительностей. Времена от gateway приходят RFC3339 (UTC);
// показываем в локальной зоне браузера.

const dateTimeFmt = new Intl.DateTimeFormat('ru-RU', {
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

const timeFmt = new Intl.DateTimeFormat('ru-RU', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

export function formatDateTime(iso?: string): string {
  if (!iso)
    return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : dateTimeFmt.format(d)
}

export function formatTime(iso?: string): string {
  if (!iso)
    return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : timeFmt.format(d)
}

export function formatTimestampMs(ms: string | number): string {
  const n = Number(ms)
  return Number.isFinite(n) ? timeFmt.format(new Date(n)) : '—'
}

// formatDuration — длительность между двумя моментами, «1м 23с».
export function formatDuration(fromIso?: string, toIso?: string): string {
  if (!fromIso || !toIso)
    return '—'
  const ms = new Date(toIso).getTime() - new Date(fromIso).getTime()
  if (!Number.isFinite(ms) || ms < 0)
    return '—'

  const totalSec = Math.round(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60

  if (h > 0)
    return `${h}ч ${m}м ${s}с`
  if (m > 0)
    return `${m}м ${s}с`
  return `${s}с`
}
