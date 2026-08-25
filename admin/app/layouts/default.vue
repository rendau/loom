<script setup lang="ts">
import type { DropdownMenuItem, NavigationMenuItem } from '@nuxt/ui'

const { me, isAdmin, logout } = useAuth()

const navItems = computed<NavigationMenuItem[][]>(() => {
  const items: NavigationMenuItem[] = [
    { label: 'Обзор', icon: 'i-lucide-layout-dashboard', to: '/' },
    { label: 'Даги', icon: 'i-lucide-workflow', to: '/dags' },
    { label: 'Раны', icon: 'i-lucide-list', to: '/runs' },
    { label: 'Переменные и секреты', icon: 'i-lucide-key-round', to: '/env' },
  ]
  // администрирование — отдельной группой в конце (design/03)
  const adminItems: NavigationMenuItem[] = [
    { label: 'Пулы', icon: 'i-lucide-layers', to: '/pools' },
  ]
  if (isAdmin.value) {
    adminItems.push({ label: 'Настройки', icon: 'i-lucide-settings', to: '/settings' })
    adminItems.push({ label: 'Пользователи', icon: 'i-lucide-users', to: '/users' })
  }
  return [items, adminItems]
})

const { theme, setTheme, themes } = useLoomTheme()

// Выбор цветовой темы: галочка у активной (type: 'checkbox' — Nuxt UI
// сам рисует индикатор), подпись — характер палитры.
const themeMenuItems = computed<DropdownMenuItem[][]>(() => [
  themes.map(t => ({
    label: t.label,
    description: t.description,
    type: 'checkbox' as const,
    checked: theme.value === t.value,
    onSelect: (e: Event) => {
      // без preventDefault меню закрывается раньше, чем применится тема
      e.preventDefault()
      setTheme(t.value)
    },
  })),
])

const userMenuItems = computed<DropdownMenuItem[][]>(() => [[
  {
    label: me.value?.username ?? '',
    // роль подписью, чтобы было видно права текущей сессии
    description: me.value?.role === 'admin' ? 'администратор' : 'пользователь',
    icon: 'i-lucide-user',
    type: 'label' as const,
  },
], [
  { label: 'Выйти', icon: 'i-lucide-log-out', onSelect: () => logout() },
]])
</script>

<template>
  <UDashboardGroup unit="rem">
    <UDashboardSidebar
      collapsible
      :min-size="10"
      :default-size="13"
      :max-size="18"
      class="group"
      :ui="{
        header: 'group-data-[collapsed=true]:px-2',
        body: 'group-data-[collapsed=true]:px-2',
      }"
    >
      <template #header="{ collapsed }">
        <NuxtLink v-if="!collapsed" to="/" class="flex items-center gap-2 font-bold text-highlighted">
          <UIcon name="i-lucide-shell" class="size-5 text-primary" />
          loom
        </NuxtLink>
        <UDashboardSidebarCollapse :class="collapsed ? 'mx-auto' : 'ms-auto'" />
      </template>

      <template #default="{ collapsed }">
        <UNavigationMenu
          :collapsed="collapsed"
          :items="navItems"
          orientation="vertical"
          :ui="{ linkLabel: 'whitespace-normal' }"
        />
      </template>

      <template #footer="{ collapsed }">
        <!-- свёрнутый сайдбар — узкая колонка: профиль и тема друг под другом -->
        <div class="flex w-full gap-1" :class="collapsed ? 'flex-col' : 'items-center'">
          <UDropdownMenu :items="userMenuItems" :class="collapsed ? 'w-full' : 'flex-1 min-w-0'">
            <UButton
              icon="i-lucide-circle-user"
              :label="collapsed ? undefined : (me?.username ?? 'Профиль')"
              color="neutral"
              variant="ghost"
              block
            />
          </UDropdownMenu>

          <UDropdownMenu :items="themeMenuItems" :class="collapsed ? 'w-full' : undefined">
            <UButton
              icon="i-lucide-palette"
              color="neutral"
              variant="ghost"
              :block="collapsed"
              aria-label="Цветовая тема"
            />
          </UDropdownMenu>
        </div>
      </template>
    </UDashboardSidebar>

    <slot />
  </UDashboardGroup>
</template>
