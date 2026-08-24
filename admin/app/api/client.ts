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

// apiErrorMessage — человекочитаемое сообщение из ErrorRep-тела ошибки.
export function apiErrorMessage(error: unknown): string {
  if (error instanceof FetchError) {
    const rep = error.data as ErrorRep | undefined
    if (rep?.message)
      return rep.message
    if (rep?.code)
      return rep.code
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
