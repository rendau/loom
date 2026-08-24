import { authHeaders } from '~/api/client'
import type { TaskLogEntry } from '~/types/log'

// Чтение лога попытки через server-streaming ручку gateway:
// GET /run/{id}/task/{task}/attempt/{n}/log — chunked-ответ, по одному
// JSON-объекту {"result": ReadTaskLogResponse} на строку; ошибка приходит
// объектом {"error": {...}}. При follow=true стрим живёт до завершения
// попытки — так работают live-логи.
//
// Обрыв соединения переживается реконнектом: клиент считает полученные
// строки и переоткрывает стрим с after_seq — без потерь и дублей.

interface StreamMessage {
  result?: { entries?: TaskLogEntry[] }
  error?: { message?: string, details?: Array<{ message?: string }> }
}

export interface ReadTaskLogParams {
  runId: string
  task: string
  attempt: number
  follow: boolean
  // Сколько первых строк пропустить (продолжение после уже полученных).
  afterSeq?: number
}

export type LogStreamStatus = 'connected' | 'reconnecting'

// apiError — ошибка уровня API (HTTP-статус или {"error":...} в стриме):
// реконнект не поможет, пробрасываем наружу.
class apiError extends Error {}

const reconnectMinDelayMs = 1000
const reconnectMaxDelayMs = 8000

export async function readTaskLog(
  params: ReadTaskLogParams,
  onEntries: (entries: TaskLogEntry[]) => void,
  signal?: AbortSignal,
  onStatus?: (status: LogStreamStatus) => void,
): Promise<void> {
  let received = params.afterSeq ?? 0
  let delay = reconnectMinDelayMs

  for (;;) {
    try {
      onStatus?.('connected')
      await readOnce(params, received, (entries) => {
        received += entries.length
        delay = reconnectMinDelayMs
        onEntries(entries)
      }, signal)
      return
    }
    catch (error) {
      if (signal?.aborted)
        return
      if (error instanceof apiError)
        throw error

      // сетевой обрыв (рестарт server/artifact, потеря соединения) — ждём и
      // продолжаем с последней полученной строки
      onStatus?.('reconnecting')
      await sleep(delay, signal)
      if (signal?.aborted)
        return
      delay = Math.min(delay * 2, reconnectMaxDelayMs)
    }
  }
}

async function readOnce(
  params: ReadTaskLogParams,
  afterSeq: number,
  onEntries: (entries: TaskLogEntry[]) => void,
  signal?: AbortSignal,
): Promise<void> {
  const url = `${useApiBaseUrl()}/run/${encodeURIComponent(params.runId)}`
    + `/task/${encodeURIComponent(params.task)}`
    + `/attempt/${params.attempt}/log?follow=${params.follow}&after_seq=${afterSeq}`

  const response = await fetch(url, { signal, headers: authHeaders() })
  if (response.status === 401) {
    setAuthToken('')
    sessionExpired.value = true
    throw new apiError('сессия истекла — войдите заново')
  }
  if (!response.ok || !response.body)
    throw new apiError(`лог недоступен: HTTP ${response.status}`)

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  const handleLine = (line: string) => {
    if (!line.trim())
      return
    const msg = JSON.parse(line) as StreamMessage
    if (msg.error)
      throw new apiError(msg.error.details?.[0]?.message ?? msg.error.message ?? 'ошибка чтения лога')
    if (msg.result?.entries?.length)
      onEntries(msg.result.entries)
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done)
      break

    buffer += decoder.decode(value, { stream: true })

    let idx = buffer.indexOf('\n')
    while (idx >= 0) {
      handleLine(buffer.slice(0, idx))
      buffer = buffer.slice(idx + 1)
      idx = buffer.indexOf('\n')
    }
  }

  // хвост без \n: на чистом завершении это полная строка; оборванная
  // посередине уронит JSON.parse — реконнект дочитает её с after_seq
  handleLine(buffer)
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(done, ms)
    function done() {
      signal?.removeEventListener('abort', done)
      clearTimeout(timer)
      resolve()
    }
    signal?.addEventListener('abort', done)
  })
}
