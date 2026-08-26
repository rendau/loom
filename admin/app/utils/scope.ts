import type { DagRef, Scope } from '~/types/common'

// Скоупы значений (переменные, секреты, настройки) и составной
// идентификатор дага: одни и те же хелперы нужны и страницам, и карточкам,
// поэтому живут в utils (auto-import), а типы — в types/common.

export const globalScope: Scope = { project: '', dag: '' }

export function projectScope(project: string): Scope {
  return { project, dag: '' }
}

export function dagScope(ref: DagRef): Scope {
  return { project: ref.project, dag: ref.name }
}

export function scopeKind(scope: Scope): 'global' | 'project' | 'dag' {
  if (scope.project && scope.dag)
    return 'dag'
  return scope.project ? 'project' : 'global'
}

// Человекочитаемая форма скоупа: '' — глобальный, «проект» или «проект/даг».
export function scopeLabel(scope: Scope): string {
  if (scope.project && scope.dag)
    return `${scope.project}/${scope.dag}`
  return scope.project
}

// Разбор человекочитаемой формы (query-параметры, ссылки).
export function parseScopeLabel(value: string): Scope {
  if (!value)
    return globalScope
  const [project = '', dag = ''] = value.split('/')
  return { project, dag }
}

export function scopeEq(a: Scope, b: Scope): boolean {
  return a.project === b.project && a.dag === b.dag
}

// Скоуп записи → даг, если это скоуп дага (для проверки прав и ссылок).
export function scopeDagRef(scope: Scope): DagRef | undefined {
  return scopeKind(scope) === 'dag' ? { project: scope.project, name: scope.dag } : undefined
}

export function dagRefLabel(ref: DagRef): string {
  return `${ref.project}/${ref.name}`
}

export function dagRefEq(a: DagRef, b: DagRef): boolean {
  return a.project === b.project && a.name === b.name
}

// Ссылка на карточку дага: путь составной, как и идентификатор.
export function dagLink(ref: DagRef, query = ''): string {
  const path = `/dags/${encodeURIComponent(ref.project)}/${encodeURIComponent(ref.name)}`
  return query ? `${path}?${query}` : path
}

// Даг рана: ран хранит проект и имя инстанса отдельными полями.
export function runDagRef(run: { project: string, dag_name: string }): DagRef {
  return { project: run.project, name: run.dag_name }
}

// Разбор метки «проект/даг» в ссылку на даг (query-фильтры, селекты).
export function parseDagLabel(label: string): DagRef {
  const scope = parseScopeLabel(label)
  return { project: scope.project, name: scope.dag }
}

// Ссылка на страницу шаблона (дага в образе): его манифест общий для всех
// заведённых инстансов, поэтому живёт под проектом, а не под дагом.
export function templateLink(project: string, template: string): string {
  return `/projects/${encodeURIComponent(project)}/templates/${encodeURIComponent(template)}`
}
