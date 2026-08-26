import type { DagRef, Scope } from '~/types/common'
import type { Dag, DagTask } from '~/types/dag'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Требования дага к окружению: что за переменные и секреты он объявил в
// коде (манифест `describe`) и заведено ли соответствующее значение в
// админке. Это ответ на «какие переменные нужны этому дагу» — раньше его
// приходилось спрашивать у автора дага.
//
// В отличие от utils/runenv (снапшот конкретного рана, ключ — env-имя в
// контейнере) здесь агрегируем по ИМЕНИ переменной/секрета на control
// plane: заполняют именно его, а один и тот же секрет могут читать
// несколько тасков под разными env-именами.
//
// Правило резолва — то же, что при launch: запись дага перекрывает
// запись проекта, а та — глобальную с тем же именем.

export type EnvKind = 'variable' | 'secret'

export interface DagEnvRequirement {
  kind: EnvKind
  name: string // имя переменной/секрета на control plane
  // описание из кода дага (опция loom.Variable/loom.Secret); '' — автор
  // дага его не задал
  description: string
  envs: string[] // env-имена в контейнерах тасков
  tasks: string[] // какие таски требуют
  // Скоуп-источник значения; undefined — записи нет ни на одном уровне:
  // запуск таска упадёт launch_failed
  scope?: Scope
  value?: string // текущее значение переменной (у секретов — никогда)
}

// Цепочка скоупов дага от самого узкого к глобальному — тот же порядок,
// что у резолва на сервере.
function scopeChain(ref: DagRef): Scope[] {
  return [
    { project: ref.project, dag: ref.name },
    { project: ref.project, dag: '' },
    { project: '', dag: '' },
  ]
}

function pickScope<T extends { name: string, scope: Scope }>(
  list: T[],
  name: string,
  ref: DagRef,
): T | undefined {
  for (const sc of scopeChain(ref)) {
    const found = list.find(v => v.name === name
      && v.scope.project === sc.project && v.scope.dag === sc.dag)
    if (found)
      return found
  }
  return undefined
}

export function resolveDagEnv(
  tasks: DagTask[],
  ref: DagRef,
  variables: Variable[],
  secrets: SecretMeta[],
): DagEnvRequirement[] {
  const out = new Map<string, DagEnvRequirement>()

  const add = (kind: EnvKind, name: string, env: string, description: string, task: string) => {
    const key = `${kind}:${name}`
    let req = out.get(key)
    if (!req) {
      req = { kind, name, description: '', envs: [], tasks: [] }
      if (kind === 'variable') {
        const v = pickScope(variables, name, ref)
        req.scope = v?.scope
        req.value = v?.value
      }
      else {
        req.scope = pickScope(secrets, name, ref)?.scope
      }
      out.set(key, req)
    }
    // описание объявляется у таска; если тасков несколько — берём первое
    // непустое, чтобы не терять подсказку из-за порядка объявления
    if (!req.description && description)
      req.description = description
    if (!req.envs.includes(env))
      req.envs.push(env)
    if (!req.tasks.includes(task))
      req.tasks.push(task)
  }

  for (const t of tasks) {
    for (const b of t.variables ?? [])
      add('variable', b.variable, b.env, b.description ?? '', t.name)
    for (const b of t.secrets ?? [])
      add('secret', b.secret, b.env, b.description ?? '', t.name)
  }

  // незаполненные — наверх: это то, что требует действия
  return [...out.values()].sort((a, b) => {
    const missing = Number(a.scope === undefined) - Number(b.scope === undefined)
    if (missing !== 0)
      return -missing
    return a.name.localeCompare(b.name)
  })
}

export function countMissingEnv(reqs: DagEnvRequirement[]): number {
  return reqs.filter(r => r.scope === undefined).length
}

// Сколько незаполненных у каждого дага — для бейджей списка и обзора.
// Ключ — «проект/даг»: имена инстансов уникальны только внутри проекта.
export function missingEnvByDag(
  dags: Dag[],
  variables: Variable[],
  secrets: SecretMeta[],
): Map<string, number> {
  const out = new Map<string, number>()
  for (const d of dags) {
    const missing = countMissingEnv(resolveDagEnv(d.tasks ?? [], d, variables, secrets))
    if (missing > 0)
      out.set(dagRefLabel(d), missing)
  }
  return out
}
