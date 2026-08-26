<script setup lang="ts">
import type { DropdownMenuItem, NavigationMenuItem } from '@nuxt/ui'

const { me, isAdmin, logout } = useAuth()

const route = useRoute()

// Раздел остаётся выделенным на всех его страницах, а не только на
// списке: карточка дага, проекта, шаблона и рана лежат глубже по пути, и
// без этого сайдбар «теряет» текущее место.
function inSection(to: string): boolean {
  return to === '/' ? route.path === '/' : route.path === to || route.path.startsWith(`${to}/`)
}

function navItem(label: string, icon: string, to: string): NavigationMenuItem {
  return { label, icon, to, active: inSection(to) }
}

const navItems = computed<NavigationMenuItem[][]>(() => {
  const items: NavigationMenuItem[] = [
    navItem('Обзор', 'i-lucide-layout-dashboard', '/'),
    navItem('Проекты', 'i-lucide-package', '/projects'),
    navItem('Даги', 'i-lucide-workflow', '/dags'),
    navItem('Раны', 'i-lucide-list', '/runs'),
    navItem('Переменные и секреты', 'i-lucide-key-round', '/env'),
  ]
  // администрирование — отдельной группой в конце (design/03)
  const adminItems: NavigationMenuItem[] = [
    navItem('Пулы', 'i-lucide-layers', '/pools'),
  ]
  if (isAdmin.value) {
    adminItems.push(navItem('Настройки', 'i-lucide-settings', '/settings'))
    adminItems.push(navItem('Пользователи', 'i-lucide-users', '/users'))
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
