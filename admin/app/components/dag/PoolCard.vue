<script setup lang="ts">
import { apiErrorMessage } from '~/api/client'
import { setDagPool } from '~/api/dag.api'
import { listPools } from '~/api/pool.api'
import type { Pool } from '~/types/pool'

// Пул слотов дага: задаётся только здесь (в коде дага пула нет) и
// действует на все его таски. Резолв — при запуске рана, поэтому смена
// применяется со следующего рана.

const props = defineProps<{
  dagName: string
  pool: string // '' — пул не задан, таски уходят в общий default
  canManage: boolean
}>()

const emit = defineEmits<{ saved: [] }>()

// reka-ui Select не принимает '' как value — «пул не задан» через sentinel
const NO_POOL = '__none__'

const pools = ref<Pool[]>([])
const loadError = ref('')
const selected = ref(props.pool || NO_POOL)
const action = useApiAction()

const items = computed(() => [
  { label: 'default (общий пул)', value: NO_POOL },
  ...pools.value.filter(p => p.name !== 'default')
    .map(p => ({ label: `${p.name} · ${p.slots} слотов`, value: p.name })),
])

const dirty = computed(() => (selected.value === NO_POOL ? '' : selected.value) !== props.pool)

async function load() {
  try {
    pools.value = (await listPools()).results ?? []
    loadError.value = ''
  }
  catch (error) {
    loadError.value = apiErrorMessage(error)
  }
}

onMounted(load)

// пропс меняется после сохранения (родитель перезагружает даг)
watch(() => props.pool, v => selected.value = v || NO_POOL)

async function save() {
  const value = selected.value === NO_POOL ? '' : selected.value
  const ok = await action.run(
    () => setDagPool(props.dagName, value),
    { success: value ? `Даг привязан к пулу ${value}` : 'Пул дага снят — таски уйдут в default' },
  )
  if (ok !== undefined)
    emit('saved')
}
</script>

<template>
  <section>
    <SectionHeader title="Пул слотов" />
    <UAlert v-if="loadError" color="error" variant="subtle" :title="loadError" />
    <UCard v-else :ui="{ body: 'p-0 sm:p-0' }">
      <div class="flex flex-wrap items-center gap-3 px-4 py-2.5">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 text-sm">
            <span class="font-medium text-highlighted">Пул дага</span>
            <UBadge v-if="pool" color="info" variant="subtle" size="sm">{{ pool }}</UBadge>
            <span v-else class="text-muted">— default</span>
          </div>
          <p class="mt-0.5 text-xs text-muted">
            Действует на все таски дага: за слоты пула конкурируют таски всех привязанных к нему дагов.
          </p>
        </div>
        <template v-if="canManage">
          <USelectMenu v-model="selected" :items="items" value-key="value" class="w-64" size="sm" />
          <UButton
            size="sm"
            label="Сохранить"
            :color="dirty ? 'primary' : 'neutral'"
            :variant="dirty ? 'solid' : 'subtle'"
            :disabled="!dirty"
            :loading="action.loading.value"
            @click="save"
          />
        </template>
      </div>
    </UCard>
    <p class="mt-1.5 flex items-center gap-1 text-xs text-muted">
      <UIcon name="i-lucide-info" class="size-3.5 shrink-0" />
      <span>
        Применяется со следующего рана — у уже созданных тасков пул зафиксирован.
        Слоты пулов — в разделе <NuxtLink to="/pools" class="text-primary hover:underline">Пулы</NuxtLink>.
      </span>
    </p>
  </section>
</template>
