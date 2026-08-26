import type { Ref } from 'vue'
import type { ProjectRegistration } from '~/types/project'

// Провалившиеся регистрации за последние сутки: у нового (несозданного)
// проекта это единственное место увидеть причину. Прочитанную плашку можно
// закрыть — скрываем всё, что не новее отметки; отметка в localStorage,
// чтобы закрытое не всплывало после перезагрузки, а новый провал придёт
// позже и покажется снова.
//
// Общий для списка проектов и карточки проекта: плашка с крестиком есть на
// обеих, и без общего состояния закрытие работало бы только в одной.
const KEY = 'loom.project-reg-failed-dismissed-at'

export function useRegistrationFailures(registrations: Ref<ProjectRegistration[]>) {
  const dismissedAt = ref(Number(localStorage.getItem(KEY)) || 0)

  const failed = computed(() => {
    const since = Math.max(Date.now() - 24 * 60 * 60 * 1000, dismissedAt.value)
    return registrations.value.filter(r =>
      r.status === 'failed' && r.finished_at && new Date(r.finished_at).getTime() > since)
  })

  function dismiss() {
    if (failed.value.length === 0)
      return
    const newest = Math.max(...failed.value.map(r => new Date(r.finished_at!).getTime()))
    dismissedAt.value = newest
    localStorage.setItem(KEY, String(newest))
  }

  return { failed, dismiss }
}
