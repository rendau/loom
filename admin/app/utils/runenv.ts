import type { DagRef, Scope } from '~/types/common'
import type { DagTask } from '~/types/dag'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Резолв env-привязок тасков (из снапшота манифеста рана) в записи для
// показа: скоуп дага перекрывает проектный, проектный — глобальный (та же
// логика, что при launch). ВАЖНО: значения переменных — ТЕКУЩИЕ, снапшота значений на
// момент рана в API пока нет (design/07, требование №3) — UI обязан
// показывать пометку «могло измениться после запуска». Значения секретов
// не показываются никогда — только имя и источник.

export interface RunEnvBinding {
  env: string
  kind: 'variable' | 'secret'
  name: string // имя переменной/секрета
  // Скоуп-источник значения; undefined — запись не найдена (launch такого
  // таска упадёт launch_failed)
  scope?: Scope
  value?: string // текущее значение переменной (секреты — никогда)
}

export function resolveEnvBindings(
  tasks: DagTask[],
  ref: DagRef,
  variables: Variable[],
  secrets: SecretMeta[],
): RunEnvBinding[] {
  const out = new Map<string, RunEnvBinding>()

  const pick = <T extends { name: string, scope: Scope }>(list: T[], name: string) => {
    const chain: Scope[] = [
      { project: ref.project, dag: ref.name },
      { project: ref.project, dag: '' },
      { project: '', dag: '' },
    ]
    for (const sc of chain) {
      const found = list.find(v => v.name === name
        && v.scope.project === sc.project && v.scope.dag === sc.dag)
      if (found)
        return found
    }
    return undefined
  }

  for (const t of tasks) {
    for (const b of t.variables ?? []) {
      const key = `v:${b.env}`
      if (out.has(key))
        continue
      const v = pick(variables, b.variable)
      out.set(key, { env: b.env, kind: 'variable', name: b.variable, scope: v?.scope, value: v?.value })
    }
    for (const b of t.secrets ?? []) {
      const key = `s:${b.env}`
      if (out.has(key))
        continue
      const s = pick(secrets, b.secret)
      out.set(key, { env: b.env, kind: 'secret', name: b.secret, scope: s?.scope })
    }
  }

  return [...out.values()].sort((a, b) => a.env.localeCompare(b.env))
}
