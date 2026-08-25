import type { DagTask } from '~/types/dag'
import type { SecretMeta } from '~/types/secret'
import type { Variable } from '~/types/variable'

// Резолв env-привязок тасков (из снапшота манифеста рана) в записи для
// показа: локальный скоуп перекрывает глобальный — та же логика, что при
// launch. ВАЖНО: значения переменных — ТЕКУЩИЕ, снапшота значений на
// момент рана в API пока нет (design/07, требование №3) — UI обязан
// показывать пометку «могло измениться после запуска». Значения секретов
// не показываются никогда — только имя и источник.

export interface RunEnvBinding {
  env: string
  kind: 'variable' | 'secret'
  name: string // имя переменной/секрета
  // '' — глобальный скоуп, имя дага — локальный; undefined — запись не
  // найдена (launch такого таска упадёт launch_failed)
  scope?: string
  value?: string // текущее значение переменной (секреты — никогда)
}

export function resolveEnvBindings(
  tasks: DagTask[],
  dagName: string,
  variables: Variable[],
  secrets: SecretMeta[],
): RunEnvBinding[] {
  const out = new Map<string, RunEnvBinding>()

  const pickVariable = (name: string) =>
    variables.find(v => v.name === name && v.dag_name === dagName)
    ?? variables.find(v => v.name === name && v.dag_name === '')
  const pickSecret = (name: string) =>
    secrets.find(s => s.name === name && s.dag_name === dagName)
    ?? secrets.find(s => s.name === name && s.dag_name === '')

  for (const t of tasks) {
    for (const b of t.variables ?? []) {
      const key = `v:${b.env}`
      if (out.has(key))
        continue
      const v = pickVariable(b.variable)
      out.set(key, { env: b.env, kind: 'variable', name: b.variable, scope: v?.dag_name, value: v?.value })
    }
    for (const b of t.secrets ?? []) {
      const key = `s:${b.env}`
      if (out.has(key))
        continue
      const s = pickSecret(b.secret)
      out.set(key, { env: b.env, kind: 'secret', name: b.secret, scope: s?.dag_name })
    }
  }

  return [...out.values()].sort((a, b) => a.env.localeCompare(b.env))
}
