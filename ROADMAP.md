# loom — план и архитектурные решения

Этот файл — источник правды по направлению проекта. Новая сессия начинает
отсюда: раздел «Решения» не пересматриваем без явного запроса, чеклист
отмечаем по мере выполнения (и дополняем, если план уточнился).

## Видение

Оркестратор пайплайнов (аналог Apache Airflow), где даги пишутся на Go.

- Даг — Go-бинарник на loom SDK, упакованный в docker-образ. **Один образ =
  один даг**: код, зависимости и версия едут одним артефактом.
- Server (control plane) регистрирует даг по образу: запускает контейнер в
  режиме `describe`, получает JSON-манифест, валидирует.
- Таски выполняются запуском того же образа в отдельных контейнерах:
  `run --task=<name> --run-id=... --attempt=N`.
- Данные между тасками текут стримами через artifact-сервер, логи — стримом
  на server через SDK (без зависимости от логгера инфраструктуры).
- Самописная админка: журнал запусков, live-логи, управление дагами.

## Решения (зафиксированы, не пересматривать без запроса)

1. **Обмен данными — персистентные стримы через artifact-сервер**, не живой
   пайп и не S3. Читатель может начинать чтение до завершения (и даже до
   начала) записи — tail-follow. Данные персистятся ⇒ ретрай получателя
   читает заново с offset 0, отправителя перезапускать не нужно.
2. **Артефакт скоупится на попытку**: `(run_id, task, attempt, name)`.
   Жизненный цикл: `writing → committed | aborted`. Ретрай пишет attempt+1.
3. **Commit привязан к результату таска, а не к Close**: `return nil` —
   commit всех выходов, ошибка/panic — abort. `Close()` выхода опционален
   (только запрещает дальнейшую запись). EOF у follow-читателя = «отправитель
   успешно завершился, данные полны». Причина: «закрыл и потом упал» не должен
   оставлять читателям пол-артефакта как валидный.
4. **FinishAttempt + маркер `.done`**: по завершении попытки — abort остаткам
   её записей, читатели несозданных артефактов получают NOT_FOUND вместо
   вечного ожидания. Маркер переживает рестарт.
5. **Стейт-машина стримов реализована один раз** — `sdk/streamstore`. Её
   используют и artifact-сервер, и локальный режим SDK. Двух реализаций не
   заводить — разъедутся семантикой.
6. **Стримовое ребро — opt-in** (`AfterStreamed`): получатель ко-шедулится со
   стартом отправителя. Обычное ребро (`After`) ждёт успеха. По умолчанию —
   обычное: пайплайнинг ест слоты параллелизма и связывает отказы.
7. **Логи — стримом на server через SDK** (batched gRPC), не через docker/k8s
   логи. Дублировать в честный stdout (страховка при OOM/падении SDK).
   Перехватывать stderr процесса (dup2 fd 2) — паники Go-рантайма пишутся
   мимо логгера. Server дописывает причину смерти пода из k8s (exit code,
   OOMKilled) даже когда SDK мёртв.
8. **Executor — сразу для kubernetes, но за интерфейсом** (Launch/Watch/Kill
   на уровне attempt'а, не контейнера). **1 pod (Job c backoffLimit=0) =
   1 attempt**; ретраями управляет планировщик loom, не k8s. Лейблы
   run/task/attempt, watch через informer. В env пода: адрес server, ref
   attempt'а и короткоживущий токен, скоупленный на attempt.
9. **Локальный режим** — весь даг в одном процессе: таски горутинами,
   артефакты файлами в `.loom/runs/<run-id>/` (остаются после рана для
   изучения), run-id таймстампный. Таск общается с миром только через
   Runtime — это условие переносимости local ↔ distributed.
10. **Control plane на Postgres**, очередь тасков через
    `SELECT ... FOR UPDATE SKIP LOCKED`, без отдельного брокера.
    Runs пиннятся к digest образа на момент триггера (не к тегу).
11. **Монорепа**: единственный публикуемый Go-модуль — `sdk` (streamstore
    внутри него, чтобы sdk оставался без replace-директив). `artifact`,
    `server`, `examples` — внутренние модули с replace. Общие proto — модуль
    `api` (сгенерированный код коммитится).
12. **Стиль** — по эталону `/Users/mdb/projects/mechta/gotemplate`
    (app.App с фазами Init/Start/Listen/Stop, config через env, slog,
    Req/Rep-нейминг в proto, mobone/squirrel для Postgres — см. skill `crud`).
13. **FinishAttempt вызывает сам SDK** по завершении попытки (успех и ошибка,
    отдельным контекстом с таймаутом) — RPC `FinishAttempt` в artifact_v1.
    Control plane повторяет его как страховку при смерти пода — вызов
    идемпотентен.
14. **Remote-запись без клиентского буфера**: каждый Write уходит на сервер
    сразу (режется на chunk'и ≤256KB) — иначе tail-follow ломается: данные
    зависали бы в буфере писателя. Буферизация — забота дага (bufio.Writer).
15. **Уточнение к №11**: sdk зависит от модуля `api` обычным require без
    replace (локально резолвится через go.work); при публикации sdk
    тегируется и api (`api/vX.Y.Z`). Вторую генерацию proto внутрь sdk не
    делать — двойная регистрация дескрипторов паникует в одном бинаре.
    `go mod tidy` в sdk заработает после публикации api.
16. **Логи — best-effort**: недоступный лог-стрим не валит таск — отправка
    отключается, дубль в честный stdout продолжается. Без `LOOM_SERVER_ADDR`
    логи идут только в stdout.
17. **Cron — без catchup, at-most-once**: у дага хранится `next_run_at`;
    сдвиг — compare-and-swap (`IS NOT DISTINCT FROM`) ДО триггера: при гонке
    инстансов ран создаёт победитель CAS, упавший после сдвига триггер теряет
    тик. Регистрация и unpause пересчитывают `next_run_at` от «сейчас»
    (пропущенные тики не навёрстываются). Валидация cron — при регистрации
    (`robfig/cron/v3`, стандартные 5 полей + `@daily` и т.п.).
18. **Ретраи и таймауты — политика в манифесте таска** (`retries`,
    `retry_delay_sec`, `timeout_sec`; SDK-опции `Retries`/`RetryDelay`/
    `Timeout`). Неуспешная попытка при остатке ретраев → task_instance
    `up_for_retry` + `retry_at` (не терминальный: без finished_at, потомки
    ждут); планировщик возвращает в `queued` по времени. Backoff: база (или
    30s) × 2^(попытка−1), потолок 30m. Таймаут двухслойный: SDK отменяет
    контекст таска (виден и Runtime-операциям), control plane добивает
    зависшую running-попытку killTimedOut'ом (reason=timeout, ретраится по
    общей политике). Ресурсы (`resources`: cpu/mem request/limit, SDK-опция
    `Resources`) валидируются при регистрации как k8s quantities и уходят в
    спеку контейнера Job'а.
19. **Зомби-детект — reconcile по Job'ам, без heartbeat'а**: отдельный цикл
    (`SCHED_RECONCILE_TICK`) сверяет незавершённые попытки старше
    `SCHED_ZOMBIE_GRACE` (отсекает гонку claim → Launch) с `ListAlive`
    executor'а (List Jobs по лейблу loom). Потерянная **до старта**
    (starting без Job'а — упали между claim и Launch) → немедленный возврат
    в очередь вне политики ретраев (таск не исполнялся — ретрай не
    сжигается), reason=lost. Исчезнувшая **на бегу** (running без Job'а) →
    финализация по обычной политике, reason=pod_lost. Heartbeat из SDK не
    заводим: живой-но-зависший процесс ловит таймаут (№18), мёртвый
    контейнер — события пода, пропавший под — этот reconcile.
20. **Лог-стримы переживают рестарт server через ResumeWrite**: streamstore
    получил `ResumeWrite`/`ListWriting` — возобновление writing-стрима без
    активного писателя тем же процессом-владельцем (только для стримов,
    которыми владеет сам процесс — лог-стримы control plane; артефакты
    тасков по-прежнему лечатся lazy в aborted, их ретрай идёт новой
    попыткой). tasklog при старте резюмит все свои writing-стримы: запись
    продолжается с места обрыва, commit сделает обычная финализация (попытку,
    умершую вместе с сервером, дофинализирует зомби-детект №19).

## Состояние: сделано

- [x] SDK: DAG/Task, рёбра After/AfterStreamed, Validate, JSON-манифест (`describe`)
- [x] SDK: Runtime (Output/Input, порт artifactStore), commit по результату таска
- [x] SDK: локальный режим (`run`) на файловых стримах в `.loom/runs/`, panic → ошибка
- [x] `sdk/streamstore`: файловая стейт-машина, follow-читатели, ожидание
      несозданного артефакта, FinishAttempt/`.done`, stale-writing → aborted,
      AbortRef, DeleteRun; тесты с `-race`
- [x] proto `ArtifactService` (Write/Read/Stat/Abort/DeleteRun) + генерация в `api/`
- [x] Artifact server: gRPC handler поверх streamstore, каркас по gotemplate,
      healthcheck, Dockerfile
- [x] Пример `examples/demo-etl` (extract → stream → transform → load)
- [x] Фаза 3 — распределённый запуск таска (SDK remote):
  - `sdk/store_grpc.go` — remote `artifactStore`: Write header → chunks → commit
    (закрытие стрима без commit = abort), Read follow с offset 0; статусы
    сервера маппятся обратно в ошибки streamstore (поведение = локальному)
  - env-контракт `LOOM_*` + `run --task` в cli.go (флаги приоритетнее env;
    exit-коды: 0 успех, 1 таск упал, 2 некорректный вызов); публичная точка —
    `DAG.RunTask(ctx, TaskRunSpec)` (для executor'а, тестов, local-strict)
  - `FinishAttempt` RPC в artifact_v1 + handler; SDK вызывает по завершении
    попытки (решение №13)
  - логи: порт `logSink`; remote — batched gRPC-стрим `TaskLogService`
    (`api/proto/server_v1/task_log.proto`, батч 256 строк / 500ms) с дублем
    в честный stdout; локальный режим — как раньше, slog в stderr
  - перехват stdout/stderr процесса (dup2, `sdk/capture_unix.go`) → в logSink;
    паники рантайма и вывод мимо логгера попадают в лог-стрим
  - интеграционные тесты `artifact/test/`: tail-follow до commit через grpc,
    обычное ребро, abort упавшего писателя, NOT_FOUND несозданного артефакта
    после FinishAttempt, доставка лог-стрима (+ e2e руками: artifact-сервер
    отдельным процессом, demo-etl по таскам, ретрай новой попыткой)

- [x] Фаза 4 — control plane server (`server/`):
  - каркас по gotemplate: Postgres + mobone, единая миграция `000001_init`,
    gRPC (5052) + grpc-gateway (8082, REST для админки), system http (3004);
    vendor-proto `google/api` в `api/`, `api/proto/common` (ListParams/ErrorRep)
  - схема БД: `dag` (PK name, image, digest, manifest jsonb, schedule, paused),
    `run` (снапшот манифеста, пиннинг digest), `task_instance` (PK run+task,
    статусы pending→queued→starting→running→терминал), `attempt`
    (PK run+task+attempt, exit_code/exit_reason)
  - регистрация дага (`POST /dag`): `internal/service/dockercli` — pull →
    резолв digest (`RepoDigests`; без digest — warning и без пиннинга) →
    `docker run --rm <image> describe`; серверная валидация манифеста
    (имена, рёбра, ацикличность) — манифесту образа не доверяем
  - триггер (`POST /run`) + планировщик `internal/domain/scheduler`:
    чистый planner (`buildPlan` — promotions, каскад upstream_failed,
    завершение рана; юнит-тесты) + цикл: очередь через
    `FOR UPDATE SKIP LOCKED` (claim = переход в starting + attempt+1 +
    вставка attempt одной транзакцией), запуск через executor-порт,
    события — каналом; стримовое ребро ко-стартует по событию started
  - `K8sExecutor` (`internal/service/k8sexecutor`): Job backoffLimit=0,
    ttlSecondsAfterFinished, идентификация попытки в аннотациях (лейбл-
    значения ограничены 63 символами), informer по подам с лейблом
    `app.kubernetes.io/managed-by=loom`, exit code/OOMKilled из
    containerStatuses; дубли событий гасятся идемпотентной финализацией.
    ⚠ против живого кластера ещё не гонялся — прогнать при первом деплое
  - финализация попытки: статусы в БД (идемпотентно) → страховочный
    `FinishAttempt` на artifact-сервере → строка об исходе (source=server,
    exit code / OOMKilled) в лог + commit лог-стрима
  - логи: хранение — **streamstore** (решено: та же стейт-машина, реф
    `(run, task, attempt, "log")`, JSONL); приём `PushTaskLog` (commit
    делает планировщик при финализации, не приёмник); чтение
    `ReadTaskLog` с follow (`GET /run/{id}/task/{t}/attempt/{n}/log`);
    в enum добавлен `TASK_LOG_SOURCE_SERVER`
  - API админки: `DagService` (register/list/get/pause/delete),
    `RunService` (trigger/list/get с тасками и попытками), логи с follow;
    ошибки — `common.ErrorRep` телом HTTP-ответа
  - `EXECUTOR=none` — dev-режим без k8s (API и приём логов, раны копятся
    pending); интеграционные тесты `server/test` — полный цикл планировщика
    против Postgres (`TEST_PG_DSN`, без него skip) с фейковым executor'ом:
    happy path со стримовым ребром, каскад падения, launch-ошибка,
    идемпотентность дублей событий

## Чеклист: впереди

### Фаза 5 — надёжность планировщика

- [x] Cron-расписания (без catchup, решение №17): `next_run_at` + CAS,
      cronLoop в планировщике (`SCHED_CRON_TICK`), pause/resume останавливает
      и возобновляет расписание; `next_run_at` в API дага
- [x] Ретраи с backoff и таймауты таска (решение №18): статус `up_for_retry`
      + `retry_at`, PromoteRetries в pass'е, killTimedOut-watchdog, SDK-опции
      `Retries`/`RetryDelay`/`Timeout`, строка «retry scheduled at» в логе
      попытки; интеграционные тесты (ретрай, исчерпание, таймаут, cron с
      pause/unpause) — зелёные против Postgres
- [x] Ресурсы таска в манифесте (`Resources`, cpu/mem requests/limits):
      валидация quantities при регистрации → `resources` контейнера Job'а
- [x] Зомби-детект (решение №19 — reconcile по Job'ам вместо heartbeat):
      reconcileLoop + `ListStaleAttempts`/`ListAlive`, lost-before-start →
      переотправка в очередь без сжигания ретрая, pod_lost на бегу → обычная
      политика; интеграционные тесты обоих сценариев
- [x] Восстановление лог-стримов после рестарта server (решение №20):
      `ResumeWrite`/`ListWriting` в streamstore, tasklog резюмит свои
      writing-стримы при старте; тесты streamstore и tasklog
- [ ] Retention: TTL ранов, крон-джоб `DeleteRunArtifacts` + чистка логов
- [ ] Attempt-токены: выдача при Launch, проверка на artifact-сервере и
      лог-приёмнике (запись только в свой attempt)

### Фаза 6 — админка (`admin/`)

- [ ] Vue 3 + Naive UI + Pinia (стек как в kusec; скилл `vue-naive-admin` —
      только по явному вызову) поверх gateway API
- [ ] Журнал ранов, граф дага со статусами тасков, live-логи (follow)
- [ ] Триггер рана, ретрай таска/подграфа, pause дага

### Фаза 7 — зрелость (по мере надобности)

- [ ] Backfill / catchup, параметры рана (аналог dagrun.conf)
- [ ] XCom-подобные мелкие значения (через control plane, с лимитом размера)
- [ ] Пулы/лимиты параллелизма (per-DAG, per-task, priority)
- [ ] Secrets/connections (env-инъекция в поды)
- [ ] `DockerExecutor` (один хост, без k8s) — вторая реализация интерфейса
- [ ] Масштабирование artifact-сервера (вынесен отдельно уже сейчас);
      storage-бэкенд за интерфейсом (S3/PVC)
- [ ] `local-strict` режим: таски подпроцессами (`exec` самого себя) без docker

## Команды

```bash
make generate-proto   # protoc: api/proto → api/common, api/artifact_v1, api/server_v1 (+gateway)
make build            # artifact и server → <модуль>/cmd/build/svc
make test             # тесты sdk, artifact и server (в CI гонять с -race);
                      # интеграционные server/test требуют TEST_PG_DSN=postgres://...
cd examples && go run ./demo-etl run   # локальный прогон примера

# распределённый запуск таска против локального artifact-сервера:
DATA_DIR=/tmp/loom-data ./artifact/cmd/build/svc &
cd examples && go build -o /tmp/demo-etl ./demo-etl
LOOM_ARTIFACT_ADDR=127.0.0.1:5051 LOOM_RUN_ID=r1 /tmp/demo-etl run --task=extract

# control plane (нужен Postgres; EXECUTOR=none — без k8s):
cd server && PG_DSN=postgres://... EXECUTOR=none ./cmd/build/svc
# REST (gateway): POST /dag {image}, POST /run {dag_name}, GET /run/{id},
#                 GET /run/{id}/task/{t}/attempt/{n}/log?follow=true
```
