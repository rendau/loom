import { listDags } from '~/api/dag.api'
import { listSecrets } from '~/api/secret.api'
import { listVariables } from '~/api/variable.api'
import type { Dag } from '~/types/dag'

// Незаполненные переменные и секреты по дагам — для бейджа в списке дагов
// и блока «требует внимания» на обзоре. Даг объявляет их в коде, а значения
// заводятся в админке: пока значения нет, первый же запуск таска упадёт
// launch_failed, и узнать об этом заранее больше неоткуда.
//
// Данные меняются редко (манифест — при регистрации дага, значения —
// руками), поэтому грузим по требованию, без фонового поллинга. Ошибки —
// best effort: недоступный список не должен ронять страницу.
export function useDagEnvGaps() {
  const gaps = ref(new Map<string, number>())

  const totalMissing = computed(() => [...gaps.value.values()].reduce((sum, n) => sum + n, 0))

  // dags — уже загруженный список (страница дагов); без него грузим сами
  async function load(dags?: Dag[]) {
    try {
      const [dagList, variables, secrets] = await Promise.all([
        dags
          ? Promise.resolve(dags)
          : listDags({ list_params: { page_size: 200, sort: ['name'] } }).then(r => r.results),
        listVariables().then(r => r.results),
        listSecrets().then(r => r.results),
      ])
      gaps.value = missingEnvByDag(dagList, variables, secrets)
    }
    catch {
      gaps.value = new Map()
    }
  }

  function missing(dagName: string): number {
    return gaps.value.get(dagName) ?? 0
  }

  return { gaps, totalMissing, missing, load }
}
