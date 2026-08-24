// Гард маршрутов: без сессии — на /login; пока в системе нет ни одного
// пользователя — на /setup (первичная настройка).

const publicRoutes = new Set(['/login', '/setup'])

export default defineNuxtRouteMiddleware(async (to) => {
  const { me, usersExist, restore, fetchStatus } = useAuth()

  if (usersExist.value === null) {
    try {
      await fetchStatus()
    }
    catch {
      // API недоступен — пропускаем, страница покажет ошибку загрузки
      return
    }
  }

  if (!usersExist.value)
    return to.path === '/setup' ? undefined : navigateTo('/setup')

  if (to.path === '/setup')
    return navigateTo('/login')

  if (!me.value)
    await restore()

  if (me.value)
    return to.path === '/login' ? navigateTo('/') : undefined

  if (!publicRoutes.has(to.path))
    return navigateTo('/login')
})
