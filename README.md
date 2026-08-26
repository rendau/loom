# loom

Оркестратор пайплайнов (аналог Apache Airflow), где даги пишутся на Go.

Даг — это обычный Go-бинарник на базе loom SDK, упакованный в docker-образ:
код, зависимости и версия едут одним артефактом. Один образ («проект») может
нести несколько дагов, а от каждого дага образа («шаблона») заводят сколько
угодно дагов-инстансов — различаются они настройками и своими переменными,
код общий. Server регистрирует проект по образу (запуская его в режиме
`describe`) и выполняет таски, запуская тот же образ в отдельных контейнерах
с указанием дага и таска (`run --dag=... --task=...`).

## Структура монорепы

| Модуль | Назначение |
|---|---|
| `sdk/` | Библиотека для написания дагов: DAG/Task, Runtime, манифест, локальный режим |
| `sdk/streamstore/` | Общая стейт-машина файловых стримов — единая для artifact-сервера и локального режима |
| `api/` | Общие proto-контракты и сгенерированный код (`api/proto/`, `api/artifact_v1/`, `api/server_v1/`) |
| `artifact/` | **Artifact server** (data plane): стримовый обмен артефактами между тасками, приём и хранение логов тасков |
| `server/` | **Control plane** (stateless): проекты и даги, раны, планировщик, k8s-executor, REST/gRPC API |
| `examples/` | Примеры дагов: `demo-etl` (один даг) и `multi-dag` (два дага в одном образе) |
| `admin/` | Админка: проекты и даги, журнал запусков, логи, переменные и секреты |

## SDK

```go
func main() {
    dag := loom.New("demo_etl")

    extract := dag.Task("extract", func(ctx context.Context, rt *loom.Runtime) error {
        out, _ := rt.Output("orders") // io.WriteCloser; Close опционален
        // ... пишем стрим ...
        return nil // успех таска = commit всех его выходов
    })

    dag.Task("transform", func(ctx context.Context, rt *loom.Runtime) error {
        in, _ := rt.Input("extract", "orders") // io.ReadCloser, tail-follow
        // ... читаем по мере записи extract'ом ...
        return nil
    }, loom.AfterStreamed(extract)) // стартует одновременно с extract

    loom.Main(dag) // дагов может быть несколько: loom.Main(etl, nsiSync)
}
```

Бинарник дага сам себя описывает и выполняет:

- `describe` — печатает JSON-каталог образа: манифесты всех его дагов
  (server использует для регистрации/валидации);
- `run [--dag=<name>]` — выполняет даг целиком локально: таски горутинами,
  артефакты — файлами в `.loom/runs/<run-id>/` (разработка и отладка без
  docker и сервера; данные остаются после рана — промежуточные артефакты
  можно изучать). `--dag` обязателен, если дагов в образе несколько;
- `run --dag=... --task=... --run-id=... --attempt=N` — один таск в
  распределённом режиме; параметры приходят и через env-контракт `LOOM_*`
  (так их передаёт executor, имя дага — `LOOM_DAG`).

Правила:

- таск общается с миром **только через Runtime** (артефакты, логи) — это условие
  переносимости между локальным и распределённым режимами;
- **commit выходов привязан к результату таска**: `return nil` — все выходы
  committed, ошибка или panic — aborted. `Close()` у выхода опционален (лишь
  запрещает дальнейшую запись), забыть его — не страшно, а «закрыл и потом упал»
  не оставит читателям пол-артефакта как валидный;
- `rt.Input` разрешён только на объявленные зависимости (`After`/`AfterStreamed`);
- обычное ребро (`After`) — получатель стартует после успеха отправителя;
  стримовое (`AfterStreamed`) — одновременно с ним, данные читаются по мере записи.

## Семантика артефактов

Артефакт — персистентный стрим со скоупом **на попытку таска**
`(run_id, task, attempt, name)` и жизненным циклом:

```
writing ──commit──▶ committed   (читатели дочитают до конца и получат EOF)
   └─────abort───▶ aborted      (follow-читатели получат ошибку ABORTED)
```

- Это не живой пайп: данные персистятся, поэтому ретрай получателя просто
  читает стрим заново с offset 0, отправителя перезапускать не нужно.
- Commit происходит при успехе таска-отправителя, abort — при его ошибке:
  EOF у follow-читателя означает «отправитель успешно завершился, данные полны».
- Follow-читатель, догнавший хвост, ждёт новых данных; открывшийся раньше
  писателя — ждёт появления артефакта.
- Завершение попытки (`FinishAttempt`) ставит маркер `.done`: abort остаткам
  её записей, а читатели так и не созданных артефактов получают NOT_FOUND
  вместо вечного ожидания.
- Ретрай отправителя пишет новой попыткой (`attempt+1`) — прошлые данные не трогаются.
- Закрытие write-стрима без commit = abort (писатель упал).
- Стримы, оставшиеся `writing` после падения artifact-сервера, лениво считаются aborted.

Стейт-машина реализована один раз — в `sdk/streamstore` — и используется и
artifact-сервером, и локальным режимом SDK: семантика локально и в проде
совпадает вплоть до кода.

## Artifact server

gRPC (`ArtifactService`): `WriteArtifact` (client stream: header → chunks → commit),
`ReadArtifact` (server stream, offset + follow), `StatArtifact`, `AbortArtifact`,
`DeleteRunArtifacts` (retention). Хранение — файлы на диске + sidecar-мета,
состояние стрима в памяти синхронизирует писателя и follow-читателей.

Конфиг через env: `GRPC_PORT` (5051), `SYSTEM_HTTP_PORT` (3003, `/healthcheck`),
`DATA_DIR`, `LOG_LEVEL`, `DEBUG`. См. `artifact/.env.example`.

## Control plane server

Postgres-бэкенд (mobone), gRPC (5052) + REST через grpc-gateway (8082),
system http (3004, `/healthcheck`). Основные потоки:

- **Регистрация проекта** — `POST /project {"name": "...", "image": "..."}`:
  server делает pull, пиннит digest, запускает контейнер в режиме `describe`,
  валидирует манифест каждого дага образа (имена, рёбра, ацикличность) и
  сохраняет проект с его шаблонами; по новым шаблонам заводятся
  даги-инстансы. Перерегистрация того же проекта — новая версия образа:
  шаблоны обновляются, настройки дагов не трогаются.
- **Даг-инстанс** — `POST /dag {"project": "...", "template": "...", "name": "..."}`:
  от одного шаблона их может быть сколько угодно; идентификатор дага
  составной — `проект/имя`, отсюда пути вида `/dag/{project}/{name}`.
- **Ран** — `POST /run {"project": "...", "dag_name": "..."}`: снапшот манифеста и digest
  пиннятся на момент триггера. Планировщик раскручивает граф: очередь тасков
  в Postgres (`FOR UPDATE SKIP LOCKED`), обычное ребро ждёт успеха
  отправителя, стримовое — ко-стартует с ним. 1 pod (k8s Job,
  backoffLimit=0) = 1 attempt; события жизненного цикла — informer.
- **Финализация попытки** — статусы и exit-информация (exit code, OOMKilled)
  в БД, страховочный `FinishAttempt` на artifact-сервере, строка об исходе
  в лог попытки.
- **Логи** — SDK стримит строки на artifact-сервер
  (`artifact_v1.TaskLogService`) с seq/ack-подтверждениями: доставка без
  потерь и дублей, обрыв соединения переживается реконнектом с досылкой
  хвоста. Хранение — тот же streamstore (реф `(run, task, attempt, "log")`,
  JSONL); чтение — через control plane (прокси) с follow:
  `GET /run/{id}/task/{task}/attempt/{n}/log?follow=true` (live-логи).

Конфиг — `server/.env.example` (`PG_DSN`, `EXECUTOR=k8s|none`,
`K8S_NAMESPACE`/`K8S_KUBECONFIG`, `TASK_ARTIFACT_ADDR`/`TASK_SERVER_ADDR` —
адреса, какими их видят поды тасков, `SCHED_TICK` и др.).

## Команды

```bash
make generate-proto   # protoc: api/proto → api/common, api/artifact_v1, api/server_v1 (+gateway)
make build            # artifact и server → <модуль>/cmd/build/svc
make test             # тесты sdk, artifact и server
                      # интеграционные server/test требуют TEST_PG_DSN=postgres://...

cd examples && go run ./demo-etl describe   # манифест примера
cd examples && go run ./demo-etl run        # локальный запуск примера

cd server && PG_DSN=postgres://... EXECUTOR=none ./cmd/build/svc  # control plane без k8s
```

## Релиз SDK

Публикуются два модуля: `api` (proto-контракты) и `sdk`; sdk зависит от api
обычным require (без replace), поэтому **порядок тегирования важен**:

1. Тег `api/vX.Y.Z` на нужный коммит, пуш тега.
2. В `sdk/go.mod` зафиксировать эту версию
   (`cd sdk && go mod edit -require=github.com/rendau/loom/api@vX.Y.Z &&
   go mod tidy`), собрать/прогнать тесты, закоммитить и запушить.
3. Тег `sdk/vX.Y.Z` на коммит из п.2, пуш тега.
4. Проверка из чистого модуля вне монорепы:
   `go get github.com/rendau/loom/sdk@vX.Y.Z`.

api тегируется даже без изменений в нём (одинаковая версия у пары
api/sdk проще в сопровождении). Внутренние модули (`artifact`, `server`,
`examples`) не публикуются — живут на replace/go.work.

