package model

type ListParams struct {
	Page           int64
	PageSize       int64
	WithTotalCount bool
	OnlyCount      bool
	Sort           []string
}

// Scope — скоуп значения переменной, секрета или настройки. Три уровня:
// глобальный (пустые оба поля), проектный (только Project) и дага (оба).
// При резолве более узкий перекрывает более широкий: даг → проект →
// глобальный.
type Scope struct {
	Project string
	Dag     string
}

// GlobalScope — значение, общее для всей инсталляции.
func GlobalScope() Scope {
	return Scope{}
}

// ProjectScope — значение, общее для всех дагов одного образа.
func ProjectScope(project string) Scope {
	return Scope{Project: project}
}

// DagScope — значение конкретного дага-инстанса.
func DagScope(project, dag string) Scope {
	return Scope{Project: project, Dag: dag}
}

func (s Scope) IsGlobal() bool {
	return s.Project == "" && s.Dag == ""
}

func (s Scope) IsProject() bool {
	return s.Project != "" && s.Dag == ""
}

func (s Scope) IsDag() bool {
	return s.Project != "" && s.Dag != ""
}

// Valid — корректная комбинация: даг без проекта скоупом не является.
func (s Scope) Valid() bool {
	return s.Project != "" || s.Dag == ""
}

// Chain — порядок поиска значения от самого узкого скоупа к самому
// широкому: то, что нашлось раньше, побеждает.
func (s Scope) Chain() []Scope {
	switch {
	case s.IsDag():
		return []Scope{s, ProjectScope(s.Project), GlobalScope()}
	case s.IsProject():
		return []Scope{s, GlobalScope()}
	default:
		return []Scope{GlobalScope()}
	}
}

// String — человекочитаемая форма для логов и снапшота рана.
func (s Scope) String() string {
	switch {
	case s.IsDag():
		return s.Project + "/" + s.Dag
	case s.IsProject():
		return s.Project
	default:
		return ""
	}
}

// Kind — вид скоупа: global | project | dag (колонка scope_kind в run_env).
func (s Scope) Kind() string {
	switch {
	case s.IsDag():
		return "dag"
	case s.IsProject():
		return "project"
	default:
		return "global"
	}
}
