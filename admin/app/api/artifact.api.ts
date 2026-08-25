import { apiFetch, authHeaders } from '~/api/client'
import type { ArtifactMain, StorageStats } from '~/types/artifact'

// Артефакты ранов: листинг и статистика — обычный gateway API; содержимое —
// стримовый эндпоинт /download (скачивание файла и текстовое превью).

export function listRunArtifacts(runId: string) {
  return apiFetch<{ results?: ArtifactMain[] }>(`/run/${encodeURIComponent(runId)}/artifact`)
}

export function getStorageStats() {
  return apiFetch<StorageStats>('/storage-stats')
}

function downloadPath(runId: string, a: Pick<ArtifactMain, 'task' | 'attempt' | 'name'>): string {
  return `/run/${encodeURIComponent(runId)}/artifact/${encodeURIComponent(a.task)}`
    + `/${a.attempt}/${encodeURIComponent(a.name)}/download`
}

// Прямая ссылка скачивания: браузер стримит файл сам, без буферизации в
// JS — сессия передаётся query-параметром token (заголовок на <a href> не
// поставить).
export function artifactDownloadUrl(runId: string, a: Pick<ArtifactMain, 'task' | 'attempt' | 'name'>): string {
  return `${useApiBaseUrl()}${downloadPath(runId, a)}?token=${encodeURIComponent(getAuthToken())}`
}

// Превью первых limitBytes байт как текст (сервер отдаёт text/plain).
export async function previewArtifact(
  runId: string,
  a: Pick<ArtifactMain, 'task' | 'attempt' | 'name'>,
  limitBytes: number,
  signal?: AbortSignal,
): Promise<string> {
  const rep = await fetch(`${useApiBaseUrl()}${downloadPath(runId, a)}?limit_bytes=${limitBytes}`, {
    headers: authHeaders(),
    signal,
  })
  if (!rep.ok) {
    let message = `HTTP ${rep.status}`
    try {
      const body = await rep.json() as { message?: string, code?: string }
      message = body.message || body.code || message
    }
    catch { /* не-JSON тело — оставляем статус */ }
    throw new Error(message)
  }
  return rep.text()
}
