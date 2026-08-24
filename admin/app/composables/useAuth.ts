import * as authApi from '~/api/auth.api'
import type { User } from '~/types/user'

// Состояние сессии админки: текущий пользователь и его права. Токен живёт
// в localStorage (utils/auth), сюда попадает уже разобранный профиль.

export function useAuth() {
  const me = useState<User | null>('auth-me', () => null)
  const usersExist = useState<boolean | null>('auth-users-exist', () => null)

  const isAuthenticated = computed(() => me.value !== null)
  const isAdmin = computed(() => me.value?.role === 'admin')

  // canManageDag — право менять даг: расписание, пауза, его переменные и
  // секреты, триггер/ретрай/backfill.
  function canManageDag(dagName?: string): boolean {
    if (!me.value)
      return false
    if (me.value.role === 'admin')
      return true
    return !!dagName && me.value.dag_names.includes(dagName)
  }

  async function fetchStatus() {
    usersExist.value = (await authApi.getAuthStatus()).users_exist
    return usersExist.value
  }

  // restore — подтянуть профиль по сохранённому токену (старт приложения).
  async function restore(): Promise<boolean> {
    if (!getAuthToken()) {
      me.value = null
      return false
    }
    try {
      me.value = await authApi.getMe()
      return true
    }
    catch {
      setAuthToken('')
      me.value = null
      return false
    }
  }

  async function login(username: string, password: string) {
    const rep = await authApi.login(username, password)
    setAuthToken(rep.token)
    me.value = rep.user
    usersExist.value = true
    sessionExpired.value = false
  }

  async function setupFirstAdmin(username: string, password: string) {
    const rep = await authApi.createFirstAdmin(username, password)
    setAuthToken(rep.token)
    me.value = rep.user
    usersExist.value = true
    sessionExpired.value = false
  }

  async function logout() {
    try {
      await authApi.logout()
    }
    catch {
      // сессия могла истечь — локальный выход всё равно выполняем
    }
    setAuthToken('')
    me.value = null
    await navigateTo('/login')
  }

  return { me, usersExist, isAuthenticated, isAdmin, canManageDag, fetchStatus, restore, login, setupFirstAdmin, logout }
}
