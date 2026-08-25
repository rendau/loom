import { FetchError } from 'ofetch'
import type { ErrorRep } from '~/types/common'

// HTTP-клиент gateway API loom (grpc-gateway):
// - query-параметры: вложенные объекты — через точку
//   (list_params.page_size=50), массивы — повторением ключа, undefined не
//   сериализуется; page — 0-based;
// - ошибки — телом common.ErrorRep { code, message, fields };
// - auth — статический admin-токен заголовком Authorization: Bearer
//   (utils/auth.ts); 401 поднимает модалку ввода токена (authNeeded).

export type QueryValue = string | number | boolean | undefined | null
export type QueryParams = Record<string, QueryValue | QueryValue[] | Record<string, QueryValue | QueryValue[]>>

export interface ApiRequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  query?: QueryParams
  body?: unknown
  signal?: AbortSignal
}

// grpc-gateway ждёт плоские ключи с точками: { list_params: { page: 0 } } → 'list_params.page': 0
export function flattenQuery(params?: QueryParams): Record<string, string | string[]> {
  const out: Record<string, string | string[]> = {}
  if (!params)
    return out

  const assign = (key: string, value: QueryValue | QueryValue[]) => {
    if (value === undefined || value === null)
      return
    if (Array.isArray(value)) {
      const items = value.filter(v => v !== undefined && v !== null).map(String)
      if (items.length)
        out[key] = items
    }
    else {
      out[key] = String(value)
    }
  }

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null)
      continue

    if (typeof value === 'object' && !Array.isArray(value)) {
      for (const [subKey, subValue] of Object.entries(value))
        assign(`${key}.${subKey}`, subValue)
    }
    else {
      assign(key, value)
    }
  }

  return out
}

// Известные коды ошибок сервера (server/internal/errs) → человекочитаемые
// сообщения: сервер не всегда шлёт развёрнутый message (тогда в нём
// дублируется код).
const errorMessagesByCode: Record<string, string> = {
  service_not_available: 'Сервис недоступен',
  not_authorized: 'Требуется вход',
  invalid_credentials: 'Неверный логин или пароль',
  permission_denied: 'Недостаточно прав',
  object_not_found: 'Объект не найден',
  invalid_request: 'Некорректный запрос',
  id_required: 'Не указан идентификатор',
  dag_not_found: 'Даг не найден',
  registration_not_found: 'Запись регистрации не найдена',
  run_not_found: 'Ран не найден',
  run_not_finished: 'Ран ещё выполняется',
  run_not_running: 'Ран уже завершён',
  task_not_found: 'Таск не найден',
  task_not_retryable: 'Таск нельзя отправить на ретрай',
  invalid_manifest: 'Невалидный манифест дага',
  image_required: 'Не указан образ',
  attempt_not_found: 'Попытка не найдена',
  pool_not_found: 'Пул не найден',
  secret_not_found: 'Секрет не найден',
  variable_not_found: 'Переменная не найдена',
  user_not_found: 'Пользователь не найден',
  user_exists: 'Такой пользователь уже существует',
  value_not_found: 'Значение не найдено',
  artifact_not_found: 'Артефакт не найден',
  artifact_aborted: 'Запись артефакта была прервана',
}

// apiErrorMessage — человекочитаемое сообщение из ErrorRep-тела ошибки:
// приоритет у message сервера (там русское описание), голый код
// переводится словарём.
export function apiErrorMessage(error: unknown): string {
  if (error instanceof FetchError) {
    const rep = error.data as ErrorRep | undefined
    if (rep?.message && rep.message !== rep.code)
      return rep.message
    if (rep?.code)
      return errorMessagesByCode[rep.code] ?? rep.code
    return error.message
  }
  return String(error)
}

// authHeaders — общие заголовки запроса с токеном сессии (если есть);
// используется и стриминговым чтением логов (log.api.ts).
export function authHeaders(): Record<string, string> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = getAuthToken()
  if (token)
    headers.Authorization = `Bearer ${token}`
  return headers
}

export async function apiFetch<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  try {
    return await $fetch<T>(path, {
      baseURL: useApiBaseUrl(),
      method: options.method ?? 'GET',
      query: flattenQuery(options.query),
      body: options.body as Record<string, unknown> | undefined,
      signal: options.signal,
      headers: authHeaders(),
    })
  }
  catch (error) {
    // 401 — сессия истекла или отозвана: сбрасываем токен, гард уведёт на /login
    if (error instanceof FetchError && error.statusCode === 401) {
      setAuthToken('')
      sessionExpired.value = true
      if (import.meta.client && !window.location.pathname.startsWith('/login'))
        await navigateTo('/login')
    }
    throw error
  }
}
