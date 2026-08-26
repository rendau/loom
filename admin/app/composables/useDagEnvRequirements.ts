import type { Ref } from 'vue'
import { apiErrorMessage } from '~/api/client'
import { listSecrets } from '~/api/secret.api'
import { listVariables } from '~/api/variable.api'
import type { DagRef } from '~/types/common'
import type { DagTask } from '~/types/dag'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Требования одного дага к окружению: состав берётся из манифеста (tasks),
// заполненность — из записей переменных и секретов. Загрузку держит
// карточка дага, а не таба: счётчик «не заполнено» нужен на бейдже до
// того, как табу открыли.
export function useDagEnvRequirements(dagRef: Ref<DagRef>, tasks: Ref<DagTask[]>) {
  const variables = ref<Variable[]>([])
  const secrets = ref<SecretMeta[]>([])
  const loading = ref(false)
  const loadError = ref('')

  async function load() {
    loading.value = true
    try {
      const [v, s] = await Promise.all([listVariables(), listSecrets()])
      variables.value = v.results ?? []
      secrets.value = s.results ?? []
      loadError.value = ''
    }
    catch (error) {
      loadError.value = apiErrorMessage(error)
    }
    finally {
      loading.value = false
    }
  }

  const requirements = computed(() =>
    resolveDagEnv(tasks.value, dagRef.value, variables.value, secrets.value))

  const missing = computed(() => countMissingEnv(requirements.value))

  return { requirements, missing, loading, loadError, load }
}
