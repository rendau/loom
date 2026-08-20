# loom

Оркестратор пайплайнов (аналог Apache Airflow), где даги пишутся на Go.

Даг — это обычный Go-бинарник на базе loom SDK, упакованный в docker-образ.
Один образ = один даг: код, зависимости и версия едут одним артефактом.
Server регистрирует даг по образу (запуская его в режиме `describe`) и
выполняет таски, запуская тот же образ в отдельных контейнерах с указанием
таска (`run --task=...`).

## Структура монорепы

| Модуль | Назначение |
|---|---|
| `sdk/` | Библиотека для написания дагов: DAG/Task, Runtime, манифест, локальный режим |
| `sdk/streamstore/` | Общая стейт-машина файловых стримов — единая для artifact-сервера и локального режима |
| `api/` | Общие proto-контракты и сгенерированный код (`api/proto/`, `api/artifact_v1/`) |
| `artifact/` | **Artifact server** (data plane): стримовый обмен артефактами между тасками |
| `examples/` | Примеры дагов (`demo-etl`) |
| `server/` | *(планируется)* Control plane: расписания, раны, ретраи, executor (k8s) |
| `admin/` | *(планируется)* Админка: журнал запусков, логи, управление дагами |

## SDK

```go
func main() {
    dag := loom.New("demo_etl", loom.Schedule("0 3 * * *"))

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

    loom.Main(dag)
}
```

Бинарник дага сам себя описывает и выполняет:

- `describe` — печатает JSON-манифест (server использует для регистрации/валидации);
- `run` — выполняет даг целиком локально: таски горутинами, артефакты — файлами
  в `.loom/runs/<run-id>/` (разработка и отладка без docker и сервера; данные
  остаются после рана — промежуточные артефакты можно изучать);
- `run --task=... --run-id=... --attempt=N` — один таск в распределённом режиме
  *(появится вместе с control plane)*.

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

## Команды

```bash
make generate-proto   # protoc: api/proto → api/artifact_v1
make build            # собрать artifact server → artifact/cmd/build/svc
make test             # тесты sdk и artifact
make lint             # golangci-lint

cd examples && go run ./demo-etl describe   # манифест примера
cd examples && go run ./demo-etl run        # локальный запуск примера
```

## Дорожная карта

План, чеклист по фазам и зафиксированные архитектурные решения — в
[ROADMAP.md](ROADMAP.md). Кратко: сейчас готовы SDK с локальным режимом и
artifact-сервер; дальше — remote-бэкенд Runtime и стриминг логов (фаза 3),
control plane с k8s-executor (фаза 4), надёжность планировщика (фаза 5),
админка (фаза 6).
