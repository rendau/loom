// Сессия админки: opaque-токен от AuthService.Login в localStorage;
// api-клиент подставляет его заголовком Authorization: Bearer.
// sessionExpired — флаг «сервер ответил 401»: middleware уводит на /login.

const TOKEN_KEY = 'loom-session'

export const sessionExpired = ref(false)

export function getAuthToken(): string {
  if (import.meta.server)
    return ''
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setAuthToken(token: string): void {
  if (token)
    localStorage.setItem(TOKEN_KEY, token)
  else
    localStorage.removeItem(TOKEN_KEY)
}
