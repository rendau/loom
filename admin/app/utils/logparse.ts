// Разбор строк лога для читабельного рендера: logfmt (slog TextHandler),
// JSON-строки и ANSI-раскраска stdout/stderr. Всё эвристики: непонятная
// строка остаётся plain text.

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export interface AnsiSegment {
  text: string
  colorClass?: string
  bold?: boolean
}

export interface ParsedLogLine {
  kind: 'logfmt' | 'json' | 'text'
  level?: LogLevel
  msg?: string
  // Пары key=value logfmt (без time/level/msg) — для компактного рендера.
  fields?: Array<[string, string]>
  // Распарсенный JSON (kind=json) — для collapsible pretty-вида.
  json?: unknown
  // Строка без ANSI-кодов.
  clean: string
  // ANSI-сегменты (только если в строке были escape-коды).
  segments?: AnsiSegment[]
}

// eslint-disable-next-line no-control-regex
const ansiRe = /\u001B\[[0-9;]*m/g

export function stripAnsi(line: string): string {
  return line.replace(ansiRe, '')
}

// Базовые SGR-цвета → tailwind-классы (работают в обеих темах).
const ansiColorClasses: Record<number, string> = {
  30: 'text-neutral-500', 31: 'text-red-500', 32: 'text-green-500',
  33: 'text-yellow-500', 34: 'text-blue-500', 35: 'text-fuchsia-500',
  36: 'text-cyan-500', 37: 'text-neutral-300',
  90: 'text-neutral-400', 91: 'text-red-400', 92: 'text-green-400',
  93: 'text-yellow-400', 94: 'text-blue-400', 95: 'text-fuchsia-400',
  96: 'text-cyan-400', 97: 'text-neutral-100',
}

// parseAnsi режет строку на сегменты по SGR-кодам; null — кодов нет.
export function parseAnsi(line: string): AnsiSegment[] | null {
  if (!line.includes('\u001B'))
    return null

  const segments: AnsiSegment[] = []
  let colorClass: string | undefined
  let bold = false
  let lastIndex = 0

  ansiRe.lastIndex = 0
  for (let m = ansiRe.exec(line); m !== null; m = ansiRe.exec(line)) {
    if (m.index > lastIndex)
      segments.push({ text: line.slice(lastIndex, m.index), colorClass, bold })
    lastIndex = m.index + m[0].length

    for (const codeRaw of m[0].slice(2, -1).split(';')) {
      const code = Number(codeRaw || '0')
      if (code === 0) {
        colorClass = undefined
        bold = false
      }
      else if (code === 1) {
        bold = true
      }
      else if (ansiColorClasses[code]) {
        colorClass = ansiColorClasses[code]
      }
    }
  }
  if (lastIndex < line.length)
    segments.push({ text: line.slice(lastIndex), colorClass, bold })

  return segments
}

function normalizeLevel(raw: unknown): LogLevel | undefined {
  if (typeof raw !== 'string')
    return undefined
  const v = raw.toLowerCase()
  if (v.startsWith('deb') || v === 'trace')
    return 'debug'
  if (v.startsWith('inf'))
    return 'info'
  if (v.startsWith('warn'))
    return 'warn'
  if (v.startsWith('err') || v === 'fatal' || v === 'panic' || v === 'critical')
    return 'error'
  return undefined
}

// parseLogfmt разбирает пары key=value (значения в кавычках — с эскейпами);
// null — строка не похожа на logfmt.
function parseLogfmt(line: string): Array<[string, string]> | null {
  const pairs: Array<[string, string]> = []
  const re = /([A-Za-z0-9_.]+)=("(?:[^"\\]|\\.)*"|\S*)/g

  let covered = 0
  for (let m = re.exec(line); m !== null; m = re.exec(line)) {
    let value = m[2] ?? ''
    if (value.startsWith('"') && value.endsWith('"') && value.length >= 2) {
      try {
        value = JSON.parse(value) as string
      }
      catch {
        value = value.slice(1, -1)
      }
    }
    pairs.push([m[1]!, value])
    covered += m[0].length
  }

  // строка считается logfmt, если пары покрывают её почти целиком
  if (pairs.length < 2 || covered < line.trim().length * 0.7)
    return null
  return pairs
}

export function parseLogLine(raw: string): ParsedLogLine {
  const segments = parseAnsi(raw)
  const clean = segments ? stripAnsi(raw) : raw

  const trimmed = clean.trim()
  if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
    try {
      const json = JSON.parse(trimmed) as Record<string, unknown>
      return {
        kind: 'json',
        json,
        clean,
        segments: segments ?? undefined,
        level: normalizeLevel(json.level ?? json.lvl ?? json.severity),
        msg: typeof (json.msg ?? json.message) === 'string' ? String(json.msg ?? json.message) : undefined,
      }
    }
    catch {
      // не JSON — дальше по эвристикам
    }
  }

  const pairs = parseLogfmt(trimmed)
  if (pairs) {
    const get = (key: string) => pairs.find(([k]) => k === key)?.[1]
    const level = normalizeLevel(get('level'))
    const msg = get('msg')
    if (level || msg !== undefined) {
      return {
        kind: 'logfmt',
        level,
        msg,
        fields: pairs.filter(([k]) => k !== 'time' && k !== 'level' && k !== 'msg'),
        clean,
        segments: segments ?? undefined,
      }
    }
  }

  return { kind: 'text', clean, segments: segments ?? undefined }
}

export function logLevelColor(level?: LogLevel): 'neutral' | 'info' | 'warning' | 'error' {
  switch (level) {
    case 'warn': return 'warning'
    case 'error': return 'error'
    case 'info': return 'info'
    default: return 'neutral'
  }
}
