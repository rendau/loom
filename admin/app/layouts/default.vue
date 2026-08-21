<script setup lang="ts">
import type { NavigationMenuItem } from '@nuxt/ui'

const navItems: NavigationMenuItem[][] = [[
  { label: 'Даги', icon: 'i-lucide-workflow', to: '/dags' },
  { label: 'Раны', icon: 'i-lucide-list', to: '/runs' },
  { label: 'Пулы', icon: 'i-lucide-layers', to: '/pools' },
  { label: 'Секреты', icon: 'i-lucide-key-round', to: '/secrets' },
]]

// смена admin-токена вручную — та же модалка, что и по 401
function openTokenModal() {
  authNeeded.value = true
}
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
        <NuxtLink v-if="!collapsed" to="/dags" class="flex items-center gap-2 font-bold text-highlighted">
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
        />
      </template>

      <template #footer="{ collapsed }">
        <UButton
          icon="i-lucide-key-square"
          :label="collapsed ? undefined : 'Admin-токен'"
          color="neutral"
          variant="ghost"
          block
          @click="openTokenModal()"
        />
      </template>
    </UDashboardSidebar>

    <slot />
  </UDashboardGroup>
</template>
