package loom

import (
	"context"
	"strings"
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
	priority   int
	secrets    []secretRef
	variables  []variableRef
}

type secretRef struct {
	env    string // имя env-переменной в контейнере таска
	secret string // имя секрета на control plane
	desc   string // человекочитаемое описание (для админки), необязательно
}

type variableRef struct {
	env      string // имя env-переменной в контейнере таска
	variable string // имя переменной на control plane
	desc     string // человекочитаемое описание (для админки), необязательно
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

// Priority задаёт приоритет таска в очереди: при конкуренции за слоты
// пула таски с бОльшим приоритетом забираются первыми (по умолчанию 0).
// Сам пул задаётся в админке — на даге, а не в коде.
// В локальном режиме игнорируется.
func Priority(n int) TaskOption {
	return func(t *Task) { t.priority = n }
}

// Secret инъектит секрет control plane в env контейнера таска: значение
// секрета secretName попадёт в переменную envName. Секрет создаётся заранее
// через API/админку; отсутствующий на момент запуска секрет валит попытку
// (launch_failed). Читайте значение обычным os.Getenv. В локальном режиме
// игнорируется — задавайте переменную окружением процесса.
//
// Необязательный третий аргумент — описание секрета: оно уезжает в манифест
// и показывается в админке рядом с полем ввода значения, чтобы заполняющий
// не спрашивал у автора дага, что тут ожидается.
func Secret(envName, secretName string, description ...string) TaskOption {
	return func(t *Task) {
		t.secrets = append(t.secrets, secretRef{env: envName, secret: secretName, desc: firstDesc(description)})
	}
}

// Variable инъектит переменную control plane в env контейнера таска:
// значение переменной varName попадёт в envName. В отличие от секрета,
// значение переменной видно в админке. Скоуп резолвится на control plane:
// локальная переменная дага перекрывает глобальную с тем же именем;
// отсутствующая на момент запуска переменная валит попытку (launch_failed).
// Читайте значение обычным os.Getenv. В локальном режиме игнорируется —
// задавайте переменную окружением процесса.
//
// Необязательный третий аргумент — описание переменной: оно уезжает в
// манифест и показывается в админке рядом с полем ввода значения, чтобы
// заполняющий не спрашивал у автора дага, что тут ожидается.
func Variable(envName, varName string, description ...string) TaskOption {
	return func(t *Task) {
		t.variables = append(t.variables, variableRef{env: envName, variable: varName, desc: firstDesc(description)})
	}
}

// firstDesc — описание из вариадик-хвоста: лишние аргументы игнорируем,
// чтобы опечатка в вызове не роняла даг на ровном месте.
func firstDesc(description []string) string {
	if len(description) == 0 {
		return ""
	}
	return strings.TrimSpace(description[0])
}
