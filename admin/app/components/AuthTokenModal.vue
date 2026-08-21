<script setup lang="ts">
// Экран ввода admin-токена (env ADMIN_TOKEN на server): открывается по 401
// от API (authNeeded из utils/auth.ts) или кнопкой в сайдбаре. Сохранение —
// в localStorage + полная перезагрузка SPA: текущая страница перезапросит
// данные уже с токеном.

const token = ref('')

// локальная обёртка: присваивать auto-импортированный ref прямо в шаблоне
// (v-model) нельзя — компилируется в невалидное присваивание unref()
const open = computed({
  get: () => authNeeded.value,
  set: (v: boolean) => {
    authNeeded.value = v
  },
})

watch(authNeeded, (opened) => {
  if (opened)
    token.value = getAuthToken()
}, { immediate: true })

function save() {
  setAuthToken(token.value.trim())
  authNeeded.value = false
  window.location.reload()
}
</script>

<template>
  <UModal v-model:open="open" title="Admin-токен">
    <template #body>
      <form class="space-y-4" @submit.prevent="save">
        <p class="text-sm text-muted">
          API требует admin-токен (значение <code>ADMIN_TOKEN</code> на
          server). Токен сохранится в этом браузере.
        </p>
        <UInput
          v-model="token"
          type="password"
          placeholder="токен"
          autofocus
          class="w-full"
        />
        <div class="flex justify-end">
          <UButton type="submit" label="Сохранить" />
        </div>
      </form>
    </template>
  </UModal>
</template>
