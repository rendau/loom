// Рантайм-конфиг админки. В проде control plane server отдаёт /config.js
// (window.__APP_CONFIG__ из своих env — ADMIN_API_BASE_URL) до загрузки
// бандла; в деве значение берётся из runtimeConfig (.env → NUXT_PUBLIC_*).

interface AppConfig {
  apiBaseUrl?: string
}

declare global {
  interface Window {
    __APP_CONFIG__?: AppConfig
  }
}

function runtimeValue(key: keyof AppConfig): string {
  const fromWindow = window.__APP_CONFIG__?.[key]
  if (fromWindow)
    return fromWindow

  const publicConfig = useRuntimeConfig().public
  return String(publicConfig[key] ?? '')
}

// Базовый URL gateway API control plane (grpc-gateway, порт 8082).
export function useApiBaseUrl(): string {
  return runtimeValue('apiBaseUrl')
}
