<script setup lang="ts">
import type { DagRef } from '~/types/common'
import { apiErrorMessage } from '~/api/client'
import { deleteSetting, listSettings, setSetting } from '~/api/setting.api'
import type { Setting } from '~/types/setting'
import { perDagSettingDefs, settingValueValid } from '~/types/setting'

// Настройки хранения ранов и TTL Job'ов для конкретного дага: уточнение
// перекрывает глобальное значение (/settings). Пустое поле — действует
// глобальное.

const props = defineProps<{
  dagRef: DagRef
  canManage: boolean
}>()

const globals = ref<Setting[]>([])
const overrides = ref<Setting[]>([])
const loadError = ref('')
const action = useApiAction()

// редактируемые значения оверрайдов ('' — оверрайда нет)
const values = ref<Record<string, string>>({})

async function load() {
  try {
    const [g, o] = await Promise.all([
      listSettings(globalScope),
      listSettings(dagScope(props.dagRef)),
    ])
    globals.value = g.results ?? []
    overrides.value = o.results ?? []
    loadError.value = ''
    const next: Record<string, string> = {}
    for (const def of perDagSettingDefs)
      next[def.name] = overrides.value.find(s => s.name === def.name)?.value ?? ''
    values.value = next
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
}

onMounted(load)

function globalValue(name: string): string {
  return globals.value.find(s => s.name === name)?.value
    ?? perDagSettingDefs.find(d => d.name === name)?.default ?? ''
}

function overrideValue(name: string): string {
  return overrides.value.find(s => s.name === name)?.value ?? ''
}

function isDirty(name: string): boolean {
  return (values.value[name] ?? '').trim() !== overrideValue(name)
}

function isValid(name: string): boolean {
  const raw = (values.value[name] ?? '').trim()
  if (raw === '')
    return true // пусто — сброс к глобальному
  const def = perDagSettingDefs.find(d => d.name === name)
  return def !== undefined && settingValueValid(def, raw)
}

async function save(name: string) {
  const raw = (values.value[name] ?? '').trim()
  const hadOverride = overrideValue(name) !== ''

  const ok = raw === ''
    ? (hadOverride
        ? await action.run(() => deleteSetting(dagScope(props.dagRef), name),
          { success: 'Возврат к более широкому значению' })
        : undefined)
    : await action.run(() => setSetting(dagScope(props.dagRef), name, raw),
      { success: 'Настройка дага сохранена' })
  if (ok !== undefined)
    await load()
}
</script>

<template>
  <section>
    <SectionHeader title="Хранение и лимиты" />
    <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" />
    <UCard v-else :ui="{ body: 'p-0 sm:p-0' }">
      <div class="divide-y divide-default">
        <div v-for="def in perDagSettingDefs" :key="def.name" class="flex flex-wrap items-center gap-3 px-4 py-2.5">
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2 text-sm">
              <span class="font-medium text-highlighted">{{ def.label }}</span>
              <UBadge
                v-if="overrideValue(def.name)"
                color="info"
                variant="subtle"
                size="sm"
              >задано для дага</UBadge>
            </div>
            <p class="mt-0.5 text-xs text-muted">
              глобально: <span class="font-mono">{{ globalValue(def.name) }}</span>
            </p>
          </div>
          <template v-if="canManage">
            <UInput
              v-model="values[def.name]"
              class="w-28 font-mono"
              size="sm"
              :color="isValid(def.name) ? undefined : 'error'"
              :placeholder="globalValue(def.name)"
              :aria-label="def.label"
              @keydown.enter="isDirty(def.name) && isValid(def.name) && save(def.name)"
            />
            <UButton
              size="sm"
              label="Сохранить"
              :color="isDirty(def.name) && isValid(def.name) ? 'primary' : 'neutral'"
              :variant="isDirty(def.name) && isValid(def.name) ? 'solid' : 'subtle'"
              :disabled="!isDirty(def.name) || !isValid(def.name)"
              :loading="action.loading.value"
              @click="save(def.name)"
            />
          </template>
          <span v-else class="font-mono text-sm tabular-nums">
            {{ overrideValue(def.name) || globalValue(def.name) }}
          </span>
        </div>
      </div>
    </UCard>
    <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
      <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
      Пустое поле — действует глобальное значение (<NuxtLink to="/settings" class="text-primary hover:underline">Настройки</NuxtLink>).
      Длительности в Go-нотации: 720h, 90m, 0 — выключено.
    </p>
  </section>
</template>
