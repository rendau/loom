// parseRunParams валидирует JSON-объект параметров рана:
// undefined — параметры не заданы, null — ошибка ввода.
export function parseRunParams(raw: string): Record<string, unknown> | undefined | null {
  const trimmed = raw.trim()
  if (!trimmed)
    return undefined
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      return null
    return parsed as Record<string, unknown>
  }
  catch {
    return null
  }
}
