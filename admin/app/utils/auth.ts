// Admin-токен API (env ADMIN_TOKEN на server; пусто — auth выключен, dev):
// хранится в localStorage, api-клиент подставляет его заголовком
// Authorization: Bearer. authNeeded — флаг «сервер ответил 401»: app.vue
// показывает модалку ввода токена (AuthTokenModal).

const TOKEN_KEY = 'loom-admin-token'

export const authNeeded = ref(false)

export function getAuthToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setAuthToken(token: string): void {
  if (token)
    localStorage.setItem(TOKEN_KEY, token)
  else
    localStorage.removeItem(TOKEN_KEY)
}
