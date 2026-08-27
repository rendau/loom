# loom SDK — справочник API

Пакет: `loom "github.com/rendau/loom/sdk"`. Go 1.27. Версия SDK — `loom.Version`
(попадает в каталог образа, по ней control plane проверяет совместимость).

---

## Объявление графа

### `loom.New(name string, opts ...DAGOption) *DAG`

Создаёт даг. Ошибки объявления **не паникуют**, а копятся и возвращаются из
`Validate()` — его зовёт `Main` перед любым режимом работы.

| Опция дага | Значение |
|---|---|
| `loom.MaxActiveRuns(n)` | сколько ранов дага могут выполняться одновременно (0 — без лимита). Лишние ждут очереди. Важно для catchup/backfill, где раны создаются пачками |

Расписания, `catchup`, пула и паузы в SDK **нет** — это настройки инстанса в админке.

### `(*DAG).Task(name string, fn TaskFn, opts ...TaskOption) *Task`

```go
type TaskFn func(ctx context.Context, rt *Runtime) error
```

Возвращает `*Task` — его передают в `After`/`AfterStreamed` другого таска. Циклы
невозможны по построению: зависимость объявляется указателем на **уже созданный** таск,
поэтому объявляй таски в топологическом порядке.

| Опция таска | Значение |
|---|---|
| `loom.After(dep)` | ждать **успеха** `dep` |
| `loom.AfterStreamed(dep)` | стартовать вместе с `dep`, читать по мере записи (tail-follow) |
| `loom.Retries(n)` | ретраев после первой неудачи (всего попыток `n+1`). Сервер отклонит манифест при `n > 100` |
| `loom.RetryDelay(d)` | база паузы перед ретраем; удваивается с каждой попыткой, потолок 30 мин. По умолчанию 30 с. Гранулярность манифеста — секунды |
| `loom.Timeout(d)` | дедлайн попытки: отменяется `ctx`, зависшую попытку дополнительно убивает control plane. Таймаут-попытка ретраится обычной политикой. Гранулярность — секунды |
| `loom.Resources(spec)` | `ResourceSpec{CPURequest, CPULimit, MemoryRequest, MemoryLimit}` в нотации k8s quantities (`"500m"`, `"256Mi"`). Пустое поле — не задавать |
| `loom.Priority(n)` | приоритет в очереди пула: больше — раньше. По умолчанию 0. Локально игнорируется |
| `loom.Secret(env, name[, desc])` | значение секрета `name` control plane → env `env` контейнера |
| `loom.Variable(env, name[, desc])` | значение переменной `name` control plane → env `env` контейнера |

`Secret` и `Variable` делят одно пространство env-имён внутри таска: дубль `env` — ошибка
валидации. `desc` уезжает в манифест и показывается в админке при заполнении значения.

### `(*DAG).Validate() error`

Проверяет: имена (`^[a-z0-9][a-z0-9_-]{0,62}$`), `nil` fn, дубли тасков, чужие/самоссылочные
зависимости, отрицательные `retries`/`retryDelay`/`timeout`/`maxActiveRuns`, env-имена
(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`, запрет префикса `LOOM_`, дубли).

> `Validate` **не** ловит даг без тасков — его отклонит уже control plane
> («в даге нет тасков»). Проверяй `describe` глазами.

### `loom.Main(dags ...*DAG)`

Точка входа бинарника, последняя строка `main()`. Разбирает argv и делает `os.Exit`.
Дубль имени дага в образе — ошибка каталога.

---

## Runtime

Единственный канал таска во внешний мир по данным и логам.

| Метод | Значение |
|---|---|
| `rt.Log() *slog.Logger` | логгер с атрибутами `dag`, `run_id`, `task`, `attempt` |
| `rt.Output(name) (io.WriteCloser, error)` | открыть артефакт на запись |
| `rt.Input(task, name) (io.ReadCloser, error)` | открыть артефакт зависимости на чтение |
| `rt.PushValue(key string, v any) error` | опубликовать значение таска (JSON, ≤64KB) |
| `rt.PullValue(task, key string, dest any) error` | прочитать значение зависимости |
| `rt.Params() []byte` | параметры рана как raw JSON; `nil` — ран без параметров |
| `rt.BindParams(v any) error` | разобрать параметры в `v`; ран без параметров — **ошибка** |
| `rt.LogicalDate() time.Time` | «дата данных» рана |

Чего в `Runtime` нет: `run_id`, номера попытки и имени таска. В распределённом режиме
их можно взять из env (`LOOM_RUN_ID`, `LOOM_ATTEMPT`, `LOOM_TASK`), но в локальном режиме
этих переменных нет — для переносимого кода не полагайся на них.

### Output

- Имя валидируется тем же `nameRe`.
- Повторный `Output` с тем же именем в одной попытке — ошибка «already open».
- `Close()` **опционален** и **не коммитит**: он лишь запрещает дальнейшую запись
  (`Write` после него — ошибка «write to closed output»).
- Commit/abort всех выходов делает Runtime по результату таска, ровно один раз.
- Backpressure: `Write` блокируется, когда неподтверждённых данных накопилось 4 MiB
  (chunk — 256 KB). Данные не дропаются, пока попытка жива.

### Input

- Разрешён только для объявленных прямых зависимостей — иначе
  `task %q is not a declared dependency`.
- Читает артефакт **той попытки зависимости**, что назначил control plane
  (`LOOM_DEP_ATTEMPTS`); локально всегда попытка 1.
- Follow-семантика: `io.EOF` только после commit отправителя; abort — ошибка.

---

## CLI бинарника дага

```
<dag-binary> describe
<dag-binary> run [--dag=<name>] [--params='{...}']
<dag-binary> run [--dag=<name>] --task=<name> --run-id=<id> --attempt=<n>
```

- `describe` — печатает JSON-каталог образа в stdout. Ошибка валидации одного дага едет
  внутри каталога (`dags[].error`) и не отменяет регистрацию остальных.
- `run` без `--task` — локальный прогон целиком.
- `run --task` — распределённый режим, вызывается executor'ом; параметры берутся из env,
  флаги приоритетнее.
- `--dag` обязателен, если бинарник несёт несколько дагов (в проде его подставляет
  `LOOM_DAG`).

Коды выхода: `0` — успех, `1` — таск/ран упал, `2` — некорректный вызов или конфигурация.

---

## Env-контракт (распределённый режим)

Заполняется control plane при запуске контейнера попытки:

| Переменная | Значение |
|---|---|
| `LOOM_ARTIFACT_ADDR` | адрес artifact-сервера (артефакты и логи). **Обязателен** |
| `LOOM_SERVER_ADDR` | адрес control plane; пусто — `PushValue`/`PullValue` вернут ошибку |
| `LOOM_DAG` | имя дага (шаблона) в образе |
| `LOOM_RUN_ID`, `LOOM_TASK`, `LOOM_ATTEMPT` | координаты попытки |
| `LOOM_DEP_ATTEMPTS` | `{"task": attempt}` — чьи артефакты читать |
| `LOOM_RUN_PARAMS` | параметры рана, raw JSON |
| `LOOM_LOGICAL_DATE` | «дата данных», RFC3339 |
| `LOOM_DESCRIBE_ID` | только для describe-Job'а: включает push каталога на control plane |

Плюс env-инъекции секретов и переменных таска. Свои имена, начинающиеся с `LOOM_`,
использовать нельзя — валидация манифеста это отклонит.

---

## Программный запуск (тесты, кастомные раннеры)

```go
err := dag.RunLocal(ctx, loom.LocalDir("/tmp/runs"), loom.LocalParams([]byte(`{"day":"x"}`)))

err = dag.RunTask(ctx, loom.TaskRunSpec{
	RunID: "r1", Task: "load", Attempt: 1,
	ArtifactAddr: "127.0.0.1:5051",
	ServerAddr:   "127.0.0.1:5052",           // пусто — без значений тасков
	DepAttempts:  map[string]int{"extract": 2},
	Params:       []byte(`{"day":"x"}`),
	LogicalDate:  time.Now(),
	CaptureOutput: true,                       // перехват fd 1/2 в лог-стрим
})
```

`RunLocal` выполняет таски горутинами через `errgroup`: **первая ошибка отменяет контекст
остальных**, ран падает целиком. Ретраев в локальном режиме нет.

---

## Каталог образа (`describe`)

```json
{
  "sdk_version": "0.4.0",
  "dags": [
    {
      "name": "orders_etl",
      "manifest": {
        "name": "orders_etl",
        "max_active_runs": 1,
        "tasks": [
          {"name": "extract", "retries": 2, "timeout_sec": 1800,
           "variables": [{"env": "PG_DSN", "variable": "pg_dsn", "description": "DSN основной БД"}]},
          {"name": "load", "depends_on": [{"task": "extract"}],
           "resources": {"memory_limit": "2Gi"}}
        ]
      }
    },
    {"name": "broken_dag", "error": "task \"x\": nil fn"}
  ]
}
```

`depends_on[].streamed: true` — ребро `AfterStreamed`. Нулевые поля в JSON опускаются.

---

## Что игнорируется в локальном режиме

`Retries`, `RetryDelay`, `Priority`, `Resources`, `MaxActiveRuns`, инъекции
`Secret`/`Variable` (задавай окружением процесса). Работают: граф и рёбра, `Timeout`,
артефакты (те же файлы и та же стейт-машина `streamstore`), значения (файлы
`<dir>/<run>/values/<task>/<key>.json`), параметры, `LogicalDate` (= момент старта), логи
(в stderr).
