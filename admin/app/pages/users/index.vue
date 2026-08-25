<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { createUser, deleteUser, listUsers, updateUser } from '~/api/auth.api'
import { apiErrorMessage } from '~/api/client'
import { listDags } from '~/api/dag.api'
import type { User, UserRole } from '~/types/user'

// Пользователи админки (только для admin): роли, пароли и назначение дагов.

const { me } = useAuth()

const users = ref<User[]>([])
const dagNames = ref<string[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

async function load() {
  loading.value = true
  try {
    users.value = (await listUsers()).results ?? []
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

async function loadDagNames() {
  try {
    const rep = await listDags({ list_params: { page_size: 500, sort: ['name'] } })
    dagNames.value = rep.results.map(d => d.name)
  }
  catch {
    // список дагов не критичен — селект просто будет пустым
  }
}

onMounted(async () => {
  await Promise.all([load(), loadDagNames()])
})

// создание / редактирование
const editOpen = ref(false)
const editTarget = ref<User | null>(null)
const editUsername = ref('')
const editPassword = ref('')
const editRole = ref<UserRole>('user')
const editDags = ref<string[]>([])

const roleItems = [
  { label: 'Администратор', value: 'admin' },
  { label: 'Пользователь', value: 'user' },
]

function openCreate() {
  editTarget.value = null
  editUsername.value = ''
  editPassword.value = ''
  editRole.value = 'user'
  editDags.value = []
  editOpen.value = true
}

function openEdit(user: User) {
  editTarget.value = user
  editUsername.value = user.username
  editPassword.value = ''
  editRole.value = user.role
  editDags.value = [...user.dag_names]
  editOpen.value = true
}

async function submitEdit() {
  const target = editTarget.value

  if (!target) {
    const username = editUsername.value.trim()
    if (!username || !editPassword.value)
      return
    const ok = await action.run(() => createUser({
      username,
      password: editPassword.value,
      role: editRole.value,
      dag_names: editRole.value === 'admin' ? [] : editDags.value,
    }), { success: 'Пользователь создан' })
    if (ok !== undefined) {
      editOpen.value = false
      await load()
    }
    return
  }

  const ok = await action.run(() => updateUser(target.id, {
    password: editPassword.value || undefined,
    role: editRole.value,
    dag_names: editRole.value === 'admin' ? [] : editDags.value,
    set_dag_names: true,
  }), { success: 'Пользователь обновлён' })
  if (ok !== undefined) {
    editOpen.value = false
    await load()
  }
}

const deleteTarget = ref<User | null>(null)

async function confirmDelete() {
  const user = deleteTarget.value
  if (!user)
    return
  const ok = await action.run(() => deleteUser(user.id), { success: 'Пользователь удалён' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<User>[] = [
  { accessorKey: 'username', header: 'Логин' },
  { accessorKey: 'role', header: 'Роль' },
  { id: 'dags', header: 'Назначенные даги' },
  { accessorKey: 'created_at', header: 'Создан' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="users">
    <template #header>
      <UDashboardNavbar title="Пользователи">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" aria-label="Обновить список" @click="load" />
          <UButton icon="i-lucide-user-plus" label="Создать" @click="openCreate" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки пользователей"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <UTable :data="users" :columns="columns" :loading="loading" :ui="denseTableUi">
        <template #username-cell="{ row }">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-user" class="size-4 shrink-0 text-muted" />
            <span class="font-medium text-highlighted">{{ row.original.username }}</span>
            <UBadge v-if="row.original.id === me?.id" color="neutral" variant="subtle" size="sm">это вы</UBadge>
          </div>
        </template>

        <template #role-cell="{ row }">
          <UBadge :color="row.original.role === 'admin' ? 'primary' : 'neutral'" variant="subtle">
            {{ row.original.role === 'admin' ? 'администратор' : 'пользователь' }}
          </UBadge>
        </template>

        <template #dags-cell="{ row }">
          <span v-if="row.original.role === 'admin'" class="text-muted">все</span>
          <div v-else-if="row.original.dag_names.length" class="flex flex-wrap gap-1">
            <UBadge v-for="name in row.original.dag_names.slice(0, 5)" :key="name" color="info" variant="subtle" size="sm">
              {{ name }}
            </UBadge>
            <UTooltip
              v-if="row.original.dag_names.length > 5"
              :text="row.original.dag_names.slice(5).join(', ')"
            >
              <UBadge color="neutral" variant="subtle" size="sm">+{{ row.original.dag_names.length - 5 }}</UBadge>
            </UTooltip>
          </div>
          <span v-else class="text-muted">— (только чтение)</span>
        </template>

        <template #created_at-cell="{ row }">
          <RelativeTime :time="row.original.created_at" />
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UTooltip text="Изменить">
              <UButton icon="i-lucide-pencil" size="sm" color="neutral" variant="ghost" aria-label="Изменить" @click="openEdit(row.original)" />
            </UTooltip>
            <UTooltip v-if="row.original.id !== me?.id" text="Удалить">
              <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" aria-label="Удалить" @click="deleteTarget = row.original" />
            </UTooltip>
          </div>
        </template>
      </UTable>

      <!-- создание / изменение -->
      <UModal
        v-model:open="editOpen"
        :title="editTarget ? `Пользователь ${editUsername}` : 'Создание пользователя'"
        description="Пользователь без назначенных дагов видит всё, но ничего не меняет."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="!editTarget" label="Логин">
              <UInput v-model="editUsername" class="w-full" autofocus />
            </UFormField>
            <UFormField
              :label="editTarget ? 'Новый пароль' : 'Пароль'"
              :hint="editTarget ? 'пусто — не менять' : 'минимум 8 символов'"
            >
              <UInput v-model="editPassword" type="password" class="w-full" autocomplete="new-password" />
            </UFormField>
            <UFormField label="Роль">
              <USelect v-model="editRole" :items="roleItems" value-key="value" class="w-full" />
            </UFormField>
            <UFormField
              v-if="editRole === 'user'"
              label="Назначенные даги"
              hint="их пользователь сможет менять"
            >
              <USelectMenu v-model="editDags" :items="dagNames" multiple class="w-full" />
            </UFormField>
          </div>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="editOpen = false" />
            <UButton label="Сохранить" :loading="action.loading.value" @click="submitEdit" />
          </div>
        </template>
      </UModal>

      <!-- подтверждение удаления -->
      <UModal :open="deleteTarget !== null" title="Удалить пользователя?" @update:open="deleteTarget = null">
        <template #body>
          <p>
            Пользователь <span class="font-medium">{{ deleteTarget?.username }}</span> будет удалён,
            его сессии немедленно перестанут работать.
          </p>
        </template>
        <template #footer>
          <div class="flex w-full justify-end gap-2">
            <UButton color="neutral" variant="ghost" label="Отмена" @click="deleteTarget = null" />
            <UButton color="error" label="Удалить" :loading="action.loading.value" @click="confirmDelete" />
          </div>
        </template>
      </UModal>
    </template>
  </UDashboardPanel>
</template>
