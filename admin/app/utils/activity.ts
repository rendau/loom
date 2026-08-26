import type { DashboardDay, DashboardHour } from '~/types/dashboard'

// Окно графика активности подстраивается под возраст инсталляции: 2 недели
// — потолок, но у парка из нескольких дагов, живущих пару дней, дневной
// график вырождается в один столбик у правого края. Тогда показываем сутки
// по часам, а промежуточные случаи — усечённый ряд дней.

export interface ActivityPoint {
  key: string
  label: string // подпись под столбиком: день месяца или час
  success: number
  failed: number
  running: number
  title: string // подсказка при наведении
}

// нижняя граница дневного окна: один-два столбика читаются как случайная
// засечка, а не как ряд
const MIN_DAYS = 7

function total(p: { success: number, failed: number, running: number }): number {
  return p.success + p.failed + p.running
}

function counts(v: { success: string, failed: string, running: string }) {
  return { success: Number(v.success), failed: Number(v.failed), running: Number(v.running) }
}

function summary(when: string, c: { success: number, failed: number, running: number }): string {
  return `${when}: ${c.success} успешно, ${c.failed} провал, ${c.running} выполняется`
}

function dayPoints(days: DashboardDay[]): ActivityPoint[] {
  return days.map((d) => {
    const c = counts(d)
    return { key: d.date, label: d.date.slice(8), title: summary(d.date, c), ...c }
  })
}

// Часы показываем в таймзоне смотрящего: сервер отдаёт момент, а не
// UTC-строку, как у дней.
function hourPoints(hours: DashboardHour[]): ActivityPoint[] {
  return hours.map((h) => {
    const c = counts(h)
    const at = new Date(h.hour)
    const label = String(at.getHours()).padStart(2, '0')
    return { key: h.hour, label, title: summary(`${formatDateTime(h.hour)} — ${label}:59`, c), ...c }
  })
}

export interface ActivityWindow {
  points: ActivityPoint[]
  title: string
}

export function activityWindow(days: DashboardDay[], hours: DashboardHour[]): ActivityWindow {
  const byDay = dayPoints(days)
  const byHour = hourPoints(hours)

  // вся история в пределах суток — дневной ряд не о чем: один столбик
  const activeDays = byDay.filter(p => total(p) > 0).length
  if (activeDays <= 1 && byHour.some(p => total(p) > 0))
    return { points: byHour, title: 'Активность за сутки (по часам)' }

  const firstActive = byDay.findIndex(p => total(p) > 0)
  const tail = Math.max(0, byDay.length - MIN_DAYS)
  const points = byDay.slice(firstActive < 0 ? tail : Math.min(firstActive, tail))
  return { points, title: `Активность за ${points.length} ${daysWord(points.length)}` }
}

function daysWord(n: number): string {
  return n % 10 === 1 && n % 100 !== 11 ? 'день' : 'дней'
}
