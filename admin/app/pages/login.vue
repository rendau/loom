<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'

definePageMeta({ layout: 'auth' })

const { login } = useAuth()

const username = ref('')
const password = ref('')
const loading = ref(false)
const errorText = ref('')

async function submit() {
  if (!username.value || !password.value)
    return
  loading.value = true
  errorText.value = ''
  try {
    await login(username.value, password.value)
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
        loom
      </div>
    </template>

    <form class="space-y-4" @submit.prevent="submit">
      <UFormField label="Логин">
        <UInput v-model="username" class="w-full" autofocus autocomplete="username" />
      </UFormField>
      <UFormField label="Пароль">
        <UInput v-model="password" type="password" class="w-full" autocomplete="current-password" />
      </UFormField>

      <UAlert v-if="errorText" color="error" variant="subtle" :title="errorText" />

      <UButton type="submit" label="Войти" block :loading="loading" />
    </form>
  </UCard>
</template>
