import { authHeaders } from '~/api/client'
import type { TaskLogEntry } from '~/types/log'

// Чтение лога попытки через server-streaming ручку gateway:
// GET /run/{id}/task/{task}/attempt/{n}/log — chunked-ответ, по одному
// JSON-объекту {"result": ReadTaskLogResponse} на строку; ошибка приходит
// объектом {"error": {...}}. При follow=true стрим живёт до завершения
// попытки — так работают live-логи.

interface StreamMessage {
  result?: { entries?: TaskLogEntry[] }
  error?: { message?: string, details?: Array<{ message?: string }> }
}

export interface ReadTaskLogParams {
  runId: string
  task: string
  attempt: number
  follow: boolean
}

export async function readTaskLog(
  params: ReadTaskLogParams,
  onEntries: (entries: TaskLogEntry[]) => void,
  signal?: AbortSignal,
): Promise<void> {
  const url = `${useApiBaseUrl()}/run/${encodeURIComponent(params.runId)}`
    + `/task/${encodeURIComponent(params.task)}`
    + `/attempt/${params.attempt}/log?follow=${params.follow}`

  const response = await fetch(url, { signal, headers: authHeaders() })
  if (response.status === 401) {
    authNeeded.value = true
    throw new Error('нужен admin-токен')
  }
  if (!response.ok || !response.body)
    throw new Error(`лог недоступен: HTTP ${response.status}`)

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  const handleLine = (line: string) => {
    if (!line.trim())
      return
    const msg = JSON.parse(line) as StreamMessage
    if (msg.error)
      throw new Error(msg.error.details?.[0]?.message ?? msg.error.message ?? 'ошибка чтения лога')
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

  handleLine(buffer)
}
