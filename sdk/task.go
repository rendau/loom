package loom

import "context"

// TaskFn — тело таска. С внешним миром (данные других тасков, логи) таск
// работает только через Runtime — это условие переносимости между локальным
// и распределённым режимами.
type TaskFn func(ctx context.Context, rt *Runtime) error

type Task struct {
	dag  *DAG
	name string
	fn   TaskFn
	deps []taskDep
}

type taskDep struct {
	task     *Task
	streamed bool
}

func (t *Task) Name() string {
	return t.name
}

type TaskOption func(*Task)

// After объявляет зависимость: таск стартует после успешного завершения dep.
func After(dep *Task) TaskOption {
	return func(t *Task) { t.deps = append(t.deps, taskDep{task: dep}) }
}

// AfterStreamed объявляет стримовую зависимость: таск стартует одновременно
// со стартом dep и читает его артефакты по мере записи (tail-follow).
// Падение dep abort'ит его артефакты — читатель получит ошибку.
func AfterStreamed(dep *Task) TaskOption {
	return func(t *Task) { t.deps = append(t.deps, taskDep{task: dep, streamed: true}) }
}
