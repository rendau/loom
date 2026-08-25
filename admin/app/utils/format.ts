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

const shortDateTimeFmt = new Intl.DateTimeFormat('ru-RU', {
  day: '2-digit',
  month: '2-digit',
  year: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
})

const timeShortFmt = new Intl.DateTimeFormat('ru-RU', {
  hour: '2-digit',
  minute: '2-digit',
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

// formatDateShort — компактная абсолютная дата для колонок списков
// («25.08.26, 14:03»): без секунд и с двузначным годом, чтобы не
// раздувать таблицы на узком экране.
export function formatDateShort(iso?: string): string {
  if (!iso)
    return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : shortDateTimeFmt.format(d)
}

// formatRelative — время для списков: «когда относительно сейчас».
// Абсолютное значение показывается в tooltip (компонент RelativeTime).
// Понимает и будущее («через 2 ч» — ближайшие запуски).
export function formatRelative(iso?: string, now: number = Date.now()): string {
  if (!iso)
    return '—'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t))
    return '—'

  const diff = now - t
  if (diff >= 0) {
    if (diff < 45_000)
      return 'только что'
    if (diff < 3_600_000)
      return `${Math.floor(diff / 60_000)} мин назад`
    const startOfToday = new Date(now)
    startOfToday.setHours(0, 0, 0, 0)
    if (t >= startOfToday.getTime())
      return `сегодня ${timeShortFmt.format(t)}`
    if (t >= startOfToday.getTime() - 86_400_000)
      return `вчера ${timeShortFmt.format(t)}`
    return shortDateTimeFmt.format(t)
  }

  const ahead = -diff
  if (ahead < 60_000)
    return 'меньше минуты'
  if (ahead < 3_600_000)
    return `через ${Math.round(ahead / 60_000)} мин`
  if (ahead < 86_400_000)
    return `через ${Math.round(ahead / 3_600_000)} ч`
  return shortDateTimeFmt.format(t)
}

// formatBytes — человекочитаемый объём памяти («256.0 MiB»). Значения от
// gateway (int64) приходят строками — принимаем и их.
export function formatBytes(bytes?: string | number | null): string {
  const n = Number(bytes)
  if (bytes === undefined || bytes === null || !Number.isFinite(n))
    return '—'
  if (n < 1024)
    return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let value = n
  let unit = 'B'
  for (const u of units) {
    if (value < 1024)
      break
    value /= 1024
    unit = u
  }
  return `${value.toFixed(1)} ${unit}`
}

// formatDuration — длительность между двумя моментами, «1м 23с».
// nowMs — для живых объектов: без toIso считаем до «сейчас» (тикающая
// длительность running-рана), а без nowMs незавершённое остаётся «—».
export function formatDuration(fromIso?: string, toIso?: string, nowMs?: number): string {
  if (!fromIso || (!toIso && nowMs === undefined))
    return '—'
  const ms = (toIso ? new Date(toIso).getTime() : nowMs!) - new Date(fromIso).getTime()
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
