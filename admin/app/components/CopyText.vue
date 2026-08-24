<script setup lang="ts">
// Однострочный текст с truncate, полным значением в title и кнопкой
// копирования — для длинных image ref / digest / id.
const props = defineProps<{
  text: string
  mono?: boolean
}>()

const copied = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  try {
    await navigator.clipboard.writeText(props.text)
  }
  catch {
    return
  }
  copied.value = true
  clearTimeout(resetTimer)
  resetTimer = setTimeout(() => {
    copied.value = false
  }, 1500)
}

onBeforeUnmount(() => clearTimeout(resetTimer))
</script>

<template>
  <div class="flex min-w-0 items-center gap-1">
    <span class="min-w-0 truncate" :class="mono ? 'font-mono text-xs' : ''" :title="text">{{ text }}</span>
    <UButton
      :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
      size="xs"
      :color="copied ? 'success' : 'neutral'"
      variant="ghost"
      aria-label="Скопировать"
      @click="copy"
    />
  </div>
</template>
