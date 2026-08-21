<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { deleteSecret, listSecrets, setSecret } from '~/api/secret.api'
import type { SecretMeta } from '~/types/secret'

// Секреты для env-инъекции в поды тасков. API write-only: значение можно
// записать и удалить, но не прочитать — расшифровывает только control plane
// при запуске попытки.

const secrets = ref<SecretMeta[]>([])
const loading = ref(false)
const action = useApiAction()

async function load() {
  loading.value = true
  try {
    const rep = await listSecrets()
    secrets.value = rep.results ?? []
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

// создание / перезапись значения
const editOpen = ref(false)
const editName = ref('')
const editValue = ref('')
const editIsNew = ref(true)

function openCreate() {
  editIsNew.value = true
  editName.value = ''
  editValue.value = ''
  editOpen.value = true
}

function openRotate(secret: SecretMeta) {
  editIsNew.value = false
  editName.value = secret.name
  editValue.value = ''
  editOpen.value = true
}

async function submitEdit() {
  const name = editName.value.trim()
  if (!name || !editValue.value)
    return
  const ok = await action.run(() => setSecret(name, editValue.value), { success: 'Секрет сохранён' })
  if (ok !== undefined) {
    editOpen.value = false
    editValue.value = ''
    await load()
  }
}

const deleteTarget = ref<SecretMeta | null>(null)

async function confirmDelete() {
  const secret = deleteTarget.value
  if (!secret)
    return
  const ok = await action.run(() => deleteSecret(secret.name), { success: 'Секрет удалён' })
  if (ok !== undefined) {
    deleteTarget.value = null
    await load()
  }
}

const columns: TableColumn<SecretMeta>[] = [
  { accessorKey: 'name', header: 'Секрет' },
  { accessorKey: 'created_at', header: 'Создан' },
  { accessorKey: 'modified_at', header: 'Изменён' },
  { id: 'actions', header: '' },
]
</script>

<template>
  <UDashboardPanel id="secrets">
    <template #header>
      <UDashboardNavbar title="Секреты">
        <template #right>
          <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading" @click="load" />
          <UButton icon="i-lucide-plus" label="Создать" @click="openCreate" />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UTable :data="secrets" :columns="columns" :loading="loading">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-key-round" class="size-4 text-muted" />
            <span class="font-mono font-medium text-highlighted">{{ row.original.name }}</span>
          </div>
        </template>

        <template #created_at-cell="{ row }">
          {{ formatDateTime(row.original.created_at) }}
        </template>

        <template #modified_at-cell="{ row }">
          {{ formatDateTime(row.original.modified_at) }}
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UButton
              icon="i-lucide-rotate-ccw-key"
              size="sm"
              color="neutral"
              variant="ghost"
              label="Заменить"
              @click="openRotate(row.original)"
            />
            <UButton icon="i-lucide-trash-2" size="sm" color="error" variant="ghost" @click="deleteTarget = row.original" />
          </div>
        </template>
      </UTable>

      <div v-if="!loading && secrets.length === 0" class="p-8 text-center text-muted">
        Секретов нет. Таск подключает секрет опцией loom.Secret(envName, secretName).
      </div>

      <!-- создание / перезапись -->
      <UModal
        v-model:open="editOpen"
        :title="editIsNew ? 'Создание секрета' : `Замена значения ${editName}`"
        description="Значение шифруется на сервере и обратно через API не читается."
      >
        <template #body>
          <div class="space-y-4">
            <UFormField v-if="editIsNew" label="Имя секрета">
              <UInput v-model="editName" class="w-full" placeholder="prod-db-password" autofocus />
            </UFormField>
            <UFormField label="Значение">
              <UTextarea v-model="editValue" class="w-full font-mono" :rows="3" />
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
      <UModal :open="deleteTarget !== null" title="Удалить секрет?" @update:open="deleteTarget = null">
        <template #body>
          <p>
            Секрет <span class="font-mono font-medium">{{ deleteTarget?.name }}</span> будет удалён.
            Таски, ссылающиеся на него, перестанут запускаться (launch_failed).
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
