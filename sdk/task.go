package loom

import (
	"context"
	"time"
)

// TaskFn — тело таска. С внешним миром (данные других тасков, логи) таск
// работает только через Runtime — это условие переносимости между локальным
// и распределённым режимами.
type TaskFn func(ctx context.Context, rt *Runtime) error

type Task struct {
	dag        *DAG
	name       string
	fn         TaskFn
	deps       []taskDep
	retries    int
	retryDelay time.Duration
	timeout    time.Duration
	resources  ResourceSpec
	pool       string
	priority   int
	secrets    []secretRef
}

type secretRef struct {
	env    string // имя env-переменной в контейнере таска
	secret string // имя секрета на control plane
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

// Retries задаёт число ретраев таска после первой неудачной попытки
// (0 — без ретраев). Ретраями управляет control plane: новая попытка
// получает attempt+1 и пишет свои артефакты заново. Стримовый читатель
// упавшего отправителя падает вместе с ним — его retries имеет смысл
// держать не меньше, чем у отправителя.
func Retries(n int) TaskOption {
	return func(t *Task) { t.retries = n }
}

// RetryDelay задаёт базовую паузу перед ретраем; с каждой следующей
// попыткой пауза удваивается (экспоненциальный backoff). По умолчанию
// control plane использует 30 секунд. Гранулярность манифеста — секунды.
func RetryDelay(d time.Duration) TaskOption {
	return func(t *Task) { t.retryDelay = d }
}

// Timeout ограничивает время выполнения таска: контекст таска отменяется
// по дедлайну, а control plane дополнительно убивает попытку, зависшую
// дольше таймаута (в манифест уходит с гранулярностью в секунды).
// Таймаут-попытка ретраится по обычной политике Retries.
func Timeout(d time.Duration) TaskOption {
	return func(t *Task) { t.timeout = d }
}

// ResourceSpec — ресурсы контейнера таска в нотации kubernetes quantities
// ("500m", "256Mi"). Пустое поле — не задавать. В локальном режиме
// игнорируется.
type ResourceSpec struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

// Resources задаёт requests/limits контейнера попытки таска.
func Resources(spec ResourceSpec) TaskOption {
	return func(t *Task) { t.resources = spec }
}

// Pool относит таск к пулу слотов параллелизма control plane: таски всех
// дагов конкурируют за слоты своего пула. Пул должен существовать на
// control plane к моменту регистрации; по умолчанию — пул "default".
// В локальном режиме игнорируется.
func Pool(name string) TaskOption {
	return func(t *Task) { t.pool = name }
}

// Priority задаёт приоритет таска в очереди: при конкуренции за слоты
// таски с бОльшим приоритетом забираются первыми (по умолчанию 0).
// В локальном режиме игнорируется.
func Priority(n int) TaskOption {
	return func(t *Task) { t.priority = n }
}

// Secret инъектит секрет control plane в env контейнера таска: значение
// секрета secretName попадёт в переменную envName. Секрет создаётся заранее
// через API/админку; отсутствующий на момент запуска секрет валит попытку
// (launch_failed). Читайте значение обычным os.Getenv. В локальном режиме
// игнорируется — задавайте переменную окружением процесса.
func Secret(envName, secretName string) TaskOption {
	return func(t *Task) { t.secrets = append(t.secrets, secretRef{env: envName, secret: secretName}) }
}
