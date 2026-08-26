<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'
import { listSettings, setSetting } from '~/api/setting.api'
import type { Setting } from '~/types/setting'
import { settingDefs, settingValueValid } from '~/types/setting'

// Глобальные настройки инсталляции (хранятся в БД, менять может только
// admin). Уточнения на конкретном даге — в карточке дага.

const { isAdmin } = useAuth()

const stored = ref<Setting[]>([])
const loading = ref(false)
const loadError = ref('')
const action = useApiAction()

// редактируемые значения по имени настройки
const values = ref<Record<string, string>>({})

async function load() {
  loading.value = true
  try {
    stored.value = (await listSettings(globalScope)).results ?? []
    loadError.value = ''
    const next: Record<string, string> = {}
    for (const def of settingDefs)
      next[def.name] = stored.value.find(s => s.name === def.name)?.value ?? def.default
    values.value = next
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
  finally {
    loading.value = false
  }
}

onMounted(load)

function storedValue(name: string): string | undefined {
  return stored.value.find(s => s.name === name)?.value
}

function isDirty(name: string): boolean {
  const def = settingDefs.find(d => d.name === name)
  return (values.value[name] ?? '') !== (storedValue(name) ?? def?.default ?? '')
}

function isValid(name: string): boolean {
  const def = settingDefs.find(d => d.name === name)
  if (!def)
    return false
  const raw = (values.value[name] ?? '').trim()
  // пустое поле — «вернуть как было из коробки»: глобальное значение
  // нельзя удалить (retention и executor обязаны видеть полный набор),
  // поэтому записываем дефолт из реестра
  return raw === '' || settingValueValid(def, raw)
}

async function save(name: string) {
  const def = settingDefs.find(d => d.name === name)
  if (!def)
    return
  const raw = (values.value[name] ?? '').trim()
  const value = raw === '' ? def.default : raw

  const ok = await action.run(
    () => setSetting(globalScope, name, value),
    { success: raw === '' ? `Возвращено значение по умолчанию: ${def.default}` : 'Настройка сохранена' },
  )
  if (ok !== undefined)
    await load()
}
</script>

<template>
  <UDashboardPanel id="settings">
    <template #header>
      <UDashboardNavbar title="Настройки">
        <template #right>
          <UButton
            icon="i-lucide-refresh-cw" color="neutral" variant="ghost" :loading="loading"
            aria-label="Обновить" @click="load"
          />
        </template>
      </UDashboardNavbar>
    </template>

    <template #body>
      <UAlert
        v-if="loadError"
        color="error"
        variant="subtle"
        title="Ошибка загрузки настроек"
        :description="loadError"
        :actions="[{ label: 'Повторить', color: 'error', variant: 'soft', onClick: () => load() }]"
      />

      <div v-else class="mx-auto w-full max-w-3xl space-y-4">
        <p class="text-sm text-muted">
          Глобальные значения инсталляции: действуют на все даги и подхватываются фоновыми
          процессами без рестарта. Настройки хранения ранов и TTL Job'ов можно уточнить для
          конкретного дага в его карточке — уточнение приоритетнее.
          Пустое поле возвращает значение по умолчанию (оно же в подсказке поля): глобальное
          значение не удаляется, иначе фоновым процессам было бы нечего читать.
        </p>

        <UCard :ui="{ body: 'p-0 sm:p-0' }">
          <div class="divide-y divide-default">
            <div v-for="def in settingDefs" :key="def.name" class="flex flex-wrap items-start gap-3 p-4">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-medium text-highlighted">{{ def.label }}</span>
                  <UBadge color="neutral" variant="soft" size="sm" class="font-mono">{{ def.name }}</UBadge>
                  <UBadge v-if="def.perDag" color="info" variant="subtle" size="sm">уточняется на даге</UBadge>
                </div>
                <p class="mt-1 text-xs text-muted">{{ def.hint }}</p>
              </div>
              <div class="flex items-center gap-2">
                <UInput
                  v-model="values[def.name]"
                  class="w-32 font-mono"
                  size="sm"
                  :disabled="!isAdmin"
                  :color="isValid(def.name) ? undefined : 'error'"
                  :placeholder="def.default"
                  :aria-label="def.label"
                  @keydown.enter="isAdmin && isDirty(def.name) && isValid(def.name) && save(def.name)"
                />
                <UButton
                  v-if="isAdmin"
                  size="sm"
                  label="Сохранить"
                  :color="isDirty(def.name) && isValid(def.name) ? 'primary' : 'neutral'"
                  :variant="isDirty(def.name) && isValid(def.name) ? 'solid' : 'subtle'"
                  :disabled="!isDirty(def.name) || !isValid(def.name)"
                  :loading="action.loading.value"
                  @click="save(def.name)"
                />
              </div>
            </div>
          </div>
        </UCard>

        <p class="text-xs text-muted">
          Длительности — в Go-нотации: <code>720h</code>, <code>90m</code>, <code>0</code> — выключено.
        </p>
      </div>
    </template>
  </UDashboardPanel>
</template>
