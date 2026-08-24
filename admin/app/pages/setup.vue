<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'

// Первичная настройка: пока в системе нет пользователей, любой может
// создать первого администратора (дальше вход только по паролю).

definePageMeta({ layout: 'auth' })

const { setupFirstAdmin } = useAuth()

const username = ref('')
const password = ref('')
const passwordRepeat = ref('')
const loading = ref(false)
const errorText = ref('')

async function submit() {
  if (!username.value || !password.value)
    return
  if (password.value !== passwordRepeat.value) {
    errorText.value = 'Пароли не совпадают'
    return
  }
  loading.value = true
  errorText.value = ''
  try {
    await setupFirstAdmin(username.value, password.value)
    await navigateTo('/')
  }
  catch (error) {
    errorText.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}
</script>

<template>
  <UCard class="w-full max-w-sm">
    <template #header>
      <div class="flex items-center gap-2 font-bold text-highlighted">
        <UIcon name="i-lucide-shell" class="size-5 text-primary" />
        loom · первичная настройка
      </div>
    </template>

    <form class="space-y-4" @submit.prevent="submit">
      <p class="text-sm text-muted">
        Пользователей ещё нет. Создайте администратора — он сможет заводить
        остальных и управлять глобальными переменными и секретами.
      </p>

      <UFormField label="Логин">
        <UInput v-model="username" class="w-full" autofocus autocomplete="username" />
      </UFormField>
      <UFormField label="Пароль" hint="минимум 8 символов">
        <UInput v-model="password" type="password" class="w-full" autocomplete="new-password" />
      </UFormField>
      <UFormField label="Пароль ещё раз">
        <UInput v-model="passwordRepeat" type="password" class="w-full" autocomplete="new-password" />
      </UFormField>

      <UAlert v-if="errorText" color="error" variant="subtle" :title="errorText" />

      <UButton type="submit" label="Создать администратора" block :loading="loading" />
    </form>
  </UCard>
</template>
