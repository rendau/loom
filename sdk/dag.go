package loom

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// nameRe ограничивает имена дагов, тасков и артефактов: они попадают в пути
// artifact-сервера и в лейблы kubernetes.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// envNameRe — допустимые имена env-переменных для инъекции секретов.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type DAG struct {
	name          string
	schedule      string
	catchup       bool
	maxActiveRuns int
	tasks         map[string]*Task
	order         []string // порядок объявления тасков
	errs          []error  // ошибки, накопленные при объявлении графа
}

// New создаёт DAG. Ошибки объявления графа накапливаются и возвращаются из
// Validate — его вызывает Main перед любым режимом работы.
func New(name string, opts ...DAGOption) *DAG {
	d := &DAG{name: name, tasks: map[string]*Task{}}

	if !nameRe.MatchString(name) {
		d.errs = append(d.errs, fmt.Errorf("invalid dag name %q", name))
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

type DAGOption func(*DAG)

// Schedule задаёт cron-расписание дага.
func Schedule(cronExpr string) DAGOption {
	return func(d *DAG) { d.schedule = cronExpr }
}

// Catchup включает наверстывание пропущенных тиков расписания: после простоя
// control plane создаёт ран на каждый пропущенный тик (logical_date = тик).
// Без этой опции пропущенные тики теряются, расписание продолжается от
// «сейчас». Имеет смысл для инкрементальных по датам пайплайнов.
func Catchup() DAGOption {
	return func(d *DAG) { d.catchup = true }
}

// MaxActiveRuns ограничивает число одновременно выполняющихся ранов дага
// (0 — без лимита): лишние раны ждут своей очереди, их таски не стартуют.
// Важно для catchup и backfill, где раны создаются пачками.
func MaxActiveRuns(n int) DAGOption {
	return func(d *DAG) { d.maxActiveRuns = n }
}

func (d *DAG) Name() string {
	return d.name
}

// Task объявляет таск дага. fn выполняется в отдельном контейнере в
// распределённом режиме и в горутине — в локальном.
func (d *DAG) Task(name string, fn TaskFn, opts ...TaskOption) *Task {
	t := &Task{dag: d, name: name, fn: fn}

	if !nameRe.MatchString(name) {
		d.errs = append(d.errs, fmt.Errorf("invalid task name %q", name))
	}
	if fn == nil {
		d.errs = append(d.errs, fmt.Errorf("task %q: nil fn", name))
	}

	for _, opt := range opts {
		opt(t)
	}

	if _, ok := d.tasks[name]; ok {
		d.errs = append(d.errs, fmt.Errorf("duplicate task name %q", name))
		return t
	}

	d.tasks[name] = t
	d.order = append(d.order, name)

	return t
}

// Validate проверяет корректность объявленного графа. Циклы невозможны по
// построению: зависимость объявляется указателем на уже созданный таск.
func (d *DAG) Validate() error {
	errs := d.errs

	for _, name := range d.order {
		t := d.tasks[name]
		for _, dep := range t.deps {
			switch {
			case dep.task == nil:
				errs = append(errs, fmt.Errorf("task %q: nil dependency", name))
			case dep.task.dag != d:
				errs = append(errs, fmt.Errorf("task %q: dependency %q belongs to another dag", name, dep.task.name))
			case dep.task == t:
				errs = append(errs, fmt.Errorf("task %q: depends on itself", name))
			}
		}

		if t.retries < 0 {
			errs = append(errs, fmt.Errorf("task %q: negative retries %d", name, t.retries))
		}
		if t.retryDelay < 0 {
			errs = append(errs, fmt.Errorf("task %q: negative retry delay %s", name, t.retryDelay))
		}
		if t.timeout < 0 {
			errs = append(errs, fmt.Errorf("task %q: negative timeout %s", name, t.timeout))
		}
		if t.pool != "" && !nameRe.MatchString(t.pool) {
			errs = append(errs, fmt.Errorf("task %q: invalid pool name %q", name, t.pool))
		}

		seenEnv := map[string]bool{}
		for _, s := range t.secrets {
			switch {
			case !envNameRe.MatchString(s.env):
				errs = append(errs, fmt.Errorf("task %q: invalid secret env name %q", name, s.env))
			case strings.HasPrefix(s.env, "LOOM_"):
				errs = append(errs, fmt.Errorf("task %q: secret env %q conflicts with LOOM_* contract", name, s.env))
			case seenEnv[s.env]:
				errs = append(errs, fmt.Errorf("task %q: duplicate secret env %q", name, s.env))
			}
			seenEnv[s.env] = true
			if !nameRe.MatchString(s.secret) {
				errs = append(errs, fmt.Errorf("task %q: invalid secret name %q", name, s.secret))
			}
		}
	}

	if d.maxActiveRuns < 0 {
		errs = append(errs, fmt.Errorf("negative max active runs %d", d.maxActiveRuns))
	}

	return errors.Join(errs...)
}
