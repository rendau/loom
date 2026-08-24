<script setup lang="ts">
import type { DropdownMenuItem, NavigationMenuItem } from '@nuxt/ui'

const { me, isAdmin, logout } = useAuth()

const navItems = computed<NavigationMenuItem[][]>(() => {
  const items: NavigationMenuItem[] = [
    { label: 'Дашборд', icon: 'i-lucide-layout-dashboard', to: '/' },
    { label: 'Даги', icon: 'i-lucide-workflow', to: '/dags' },
    { label: 'Раны', icon: 'i-lucide-list', to: '/runs' },
    { label: 'Пулы', icon: 'i-lucide-layers', to: '/pools' },
    { label: 'Переменные и секреты', icon: 'i-lucide-key-round', to: '/env' },
  ]
  if (isAdmin.value)
    items.push({ label: 'Пользователи', icon: 'i-lucide-users', to: '/users' })
  return [items]
})

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
        <UDropdownMenu :items="userMenuItems" class="w-full">
          <UButton
            icon="i-lucide-circle-user"
            :label="collapsed ? undefined : (me?.username ?? 'Профиль')"
            color="neutral"
            variant="ghost"
            block
          />
        </UDropdownMenu>
      </template>
    </UDashboardSidebar>

    <slot />
  </UDashboardGroup>
</template>
