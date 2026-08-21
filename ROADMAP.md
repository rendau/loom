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
21. **Retention — единый TTL завершённых ранов** (`RUN_TTL`, 0 = выключено;
    период — `RETENTION_TICK`): отдельный компонент
    `server/internal/domain/retention` чистит просроченные раны батчами по
    100 в порядке артефакты → логи → запись БД (упавшая на данных очистка
    оставляет ран и повторяется следующим проходом; сами удаления
    идемпотентны). Работает и при `EXECUTOR=none`.
22. **Attempt-токены — stateless HMAC по общему секрету** (`AUTH_SECRET` на
    control plane и artifact-сервере; пусто — проверка выключена, dev-режим).
    Пакет `api/attempttoken`: claims `{run, task, attempt, admin, exp}`,
    подпись HMAC-SHA256, metadata-ключ `loom-token`. Планировщик выдаёт токен
    попытки в `LOOM_TOKEN` (`TOKEN_TTL`, дефолт 24h — должен покрывать самый
    долгий таск). Scope: запись/abort/finish — только свой attempt; чтение и
    stat — свой ран (таски читают выходы зависимостей); DeleteRunArtifacts —
    только admin-токен (control plane подписывает свои вызовы минутным
    admin-токеном). Лог-приёмник PushTaskLog требует токен своего attempt'а;
    ReadTaskLog — админский API, токеном не защищается (сеть/ingress).
23. **Параметры рана и логическая дата.** У рана есть `params` (произвольный
    JSON-объект ≤ 64KB, задаётся при ручном триггере/backfill, снапшотится)
    и `logical_date` — «дата данных»: у cron-рана — время тика расписания,
    у ручного — момент триггера, у backfill — тик периода. В env попытки:
    `LOOM_RUN_PARAMS` (raw JSON) и `LOOM_LOGICAL_DATE` (RFC3339). SDK:
    `rt.Params()` / `rt.BindParams(&v)` / `rt.LogicalDate()`; локальный
    режим — флаг `run --params='{...}'`, logical_date = время запуска.
24. **Catchup и backfill.** Манифест дага: `catchup` (SDK-опция
    `loom.Catchup()`, дефолт false — поведение №17). При catchup=true
    регистрация/unpause не сбрасывают существующий `next_run_at`, а
    cronPass двигает его CAS'ом на следующий тик после пропущенного (не от
    «сейчас») — пропущенные тики навёрстываются, максимум N за проход
    (троттлинг). Backfill — RPC `BackfillRun(dag, from, to, params?)`:
    ран на каждый cron-тик в [from, to), trigger=`backfill`,
    logical_date=тик; требует расписания; лимит тиков за вызов (защита от
    опечатки в периоде). Параллелизм backfill-ранов ограничивает
    `max_active_runs` (№26).
25. **Значения тасков (аналог XCom)** — мелкие key-value через control
    plane, не через artifact-сервер. Таблица `run_value`, скоуп
    `(run, task, key)` — ретрай перезаписывает (upsert). Значение — JSON
    ≤ 64KB. RPC `TaskValueService`: Push (токен своего attempt'а), Pull
    (токен рана), List (админка). SDK: `rt.PushValue(key, v)` /
    `rt.PullValue(task, key, &dest)` — читать можно только у объявленных
    зависимостей (как Input). Локальный режим — файлы
    `.loom/runs/<id>/values/`; распределённый без `LOOM_SERVER_ADDR` —
    явная ошибка.
26. **Пулы, приоритеты, max_active_runs.** Таблица `pool(name, slots)`,
    seed `default`/64; API List/Set (upsert), удаления нет — на пул могут
    ссылаться манифесты. Манифест таска: `pool` (дефолт `default`,
    существование проверяется при регистрации) и `priority` (больше —
    раньше из очереди); SDK-опции `Pool(name)`, `Priority(n)`. `pool` и
    `priority` денормализуются в `task_instance` при создании рана; claim
    в одной транзакции лочит пулы (`FOR UPDATE`), считает занятость
    (starting+running) и забирает queued в пределах свободных слотов,
    `ORDER BY priority DESC, queued_at`. Пер-даг лимит одновременных
    ранов — `max_active_runs` в манифесте (SDK-опция дага, 0 = без
    лимита): планировщик раскручивает только N старейших активных ранов
    дага, остальные ждут (важно для backfill). Лимиты тасков — пулами.
27. **Секреты** — env-инъекция в поды из control plane. Таблица
    `secret(name, value)`; значение шифруется AES-256-GCM (ключ — SHA-256
    от парольной фразы `SECRET_KEY`; пусто — открытым текстом, dev). API
    write-only: Set/List(имена)/Delete — значения наружу не отдаются.
    Манифест таска: `secrets: [{env, secret}]`, SDK-опция
    `loom.Secret(envName, secretName)`; env-имена валидируются (не LOOM_*),
    существование секрета при регистрации не требуется (warning). При
    Launch планировщик резолвит значения в env контейнера; отсутствующий
    секрет → launch_failed (после добавления — RetryTask). В k8s значения
    идут напрямую в спеку Job'а (не через k8s Secret) — ограничение v1.
28. **Уточнение к №8 — DockerExecutor** (`EXECUTOR=docker`): попытки —
    контейнеры на хосте через `DOCKER_BIN` (CLI, как dockercli). `docker
    run -d` с лейблами `loom=1` + run/task/attempt; события — поллинг
    (`DOCKER_POLL_TICK`): `ps -a --filter label` + inspect (exit code,
    OOMKilled); started — сразу после успешного `run`; финализированный
    контейнер удаляется; Kill — `rm -f`; ListAlive — `ps` по лейблу.
    Сеть — `DOCKER_NETWORK`; адреса planes для контейнеров — те же
    `TASK_ARTIFACT_ADDR`/`TASK_SERVER_ADDR` (напр. host.docker.internal).
29. **Регистрация дагов без docker-демона (k8s)** — describe одноразовым
    k8s Job'ом, образ тянет kubelet; не DinD-sidecar и не сокет хоста.
    Порт регистрации сужен до `Inspect(image) → (digest, manifest)` —
    pull/запуск контейнера стали деталью реализации. Выбор реализации
    следует `EXECUTOR`: k8s — `internal/service/k8sdescriber`
    (Job backoffLimit=0, activeDeadline = `K8S_DESCRIBE_TIMEOUT`, дефолт
    5m — покрывает и pull; `ttlSecondsAfterFinished` — страховка
    самоочистки при падении server'а, обычно Job удаляется сразу после
    чтения результата; лейбл `managed-by=loom-describe`, чтобы informer
    executor'а такие поды не видел); docker/none — docker-CLI
    (`dockercli`, как раньше). Манифест Job **отправляет сам** — RPC
    `DagService.PushDagManifest` с одноразовым `describe_id` (случайный
    128-бит секрет; env-контракт describe-Job'а: `LOOM_DESCRIBE_ID` +
    `LOOM_SERVER_ADDR`) — а не через логи пода: логи как канал данных
    ломаются любой печатью дага в stdout при инициализации. SDK шлёт
    манифест или ошибку валидации дага (печать манифеста в stdout
    сохраняется — диагностика через kubectl logs); id живёт, пока ждёт
    регистрация, повторная доставка отвергается, будущий админ-токен на
    PushDagManifest не распространяется (авторизация — сам id, как у
    attempt-токенов). Логи пода — только диагностика падений (хвост в
    ошибке регистрации); успешный выход пода без push — ошибка «образ со
    старым SDK». digest — из `status.containerStatuses[].imageID`
    (пустой/без digest — warning, ран не пиннится). Ошибки pull
    (ErrImagePull / ImagePullBackOff / InvalidImageName) детектятся по
    waiting-статусу контейнера — fail-fast, таймаута не ждём. (Обкатано
    на docker-desktop k8s 2026-08-21: describe-Job, push манифеста,
    digest из imageID.)

30. **Авто-обновление дагов — poll-синк в control plane** (аналог keel
    poll): у дага флаг `auto_update` (поле регистрации/админки, хранится в
    БД, НЕ в манифесте — свойство деплоя, выключается без пересборки;
    перерегистрация без явного значения его сохраняет — в API поле
    optional). Цикл `dagsync` (`DAG_SYNC_TICK`, дефолт 5m, 0 — выключен):
    для auto_update-дагов дешёвый digest-чек тега в registry (HEAD
    `/v2/<repo>/manifests/<tag>`, заголовок Docker-Content-Digest; token-
    и basic-auth, creds — docker config.json по `REGISTRY_AUTH_FILE`,
    localhost — по http) и при изменении — обычная полная перерегистрация
    (describe → валидация → сохранение). Сломанный новый образ запись не
    трогает (warning + метрика, повтор следующим тиком). Даг, чей ref
    задан digest'ом или не пиннился при регистрации, не синкается
    (warning). Семантика ранов не меняется: активные раны и RetryTask —
    на своих пиннутых digest'ах, новые — на свежем.

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
  - регистрация дага (`POST /dag`): порт `Inspect(image) → (digest,
    manifest)`; реализации — `internal/service/dockercli` (pull → резолв
    digest по `RepoDigests`; без digest — warning и без пиннинга →
    `docker run --rm <image> describe`) и `internal/service/k8sdescriber`
    (одноразовый Job, решение №29); серверная валидация манифеста
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
    Обкатан на docker-desktop k8s (2026-08-21): Jobs, informer-события,
    exit code/reason, ретраи, секреты, логи из подов
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
- [x] Retention (решение №21): `RUN_TTL`/`RETENTION_TICK`, компонент
      retention с проходами Sweep (артефакты → логи → БД), партиальный
      индекс по finished_at; интеграционный тест
- [x] Attempt-токены (решение №22): `api/attempttoken` (HMAC, claims,
      metadata `loom-token`), выдача в Launch-env, проверка scope на
      artifact-сервере и лог-приёмнике, admin-токены control plane; SDK
      прикладывает токен и к лог-стриму; тесты всех уровней

Фаза 5 закрыта.

### Фаза 6 — админка (`admin/`)

Решено (вместо Naive UI из первоначального плана): **Nuxt 4 SPA
(`ssr: false`) + Nuxt UI v4**, по образцу проекта caravaneer. Раздаётся
самим control plane server'ом на отдельном порту (`ADMIN_PORT` 8081,
статика из `ADMIN_DIR`, дефолт `./admin-ui`, `make build-admin`);
рантайм-конфиг — `/config.js` (`window.__APP_CONFIG__` из env
`ADMIN_API_BASE_URL`, задаётся после билда — один билд на все окружения);
дев-режим — `pnpm dev` + `.env` (`NUXT_PUBLIC_API_BASE_URL`) + `HTTP_CORS=true`
на gateway. Подробности — `admin/README.md`.

- [x] Каркас: Nuxt 4 + Nuxt UI v4, api-клиент (flattenQuery под
      grpc-gateway), типы-зеркала proto, раздача от server + dev-режим
- [x] Даги: список (schedule/next_run_at, пауза), регистрация по образу,
      pause/unpause, удаление, триггер рана
- [x] Журнал ранов (фильтры по дагу/статусу, пагинация), страница рана
      (таски и попытки со статусами, exit-инфо, авто-poll пока running)
- [x] Логи попытки: слайдовер со стримом ReadTaskLog (follow — live-логи),
      подсветка источников (log/stdout/stderr/server)
- [x] Граф дага со статусами тасков: `RunGetRep.manifest_tasks` (рёбра из
      снапшота манифеста рана, а не текущего дага), SVG-граф в админке —
      слоистая раскладка (ранг = длиннейший путь, барицентр в колонке),
      цвета статусов токенами Nuxt UI, стримовые рёбра пунктиром, клик по
      таску открывает лог
- [x] Ретрай таска/подграфа: RPC `RetryTask` (`POST
      /run/{id}/task/{task}/retry`) — только на завершённом ране
      (планировщик не трогает finished-раны ⇒ сброс не гонится с раскруткой
      графа): таск → queued (новая попытка attempt+1 обычным claim'ом),
      его downstream-подграф транзитивно → pending, ран реактивируется;
      старые попытки остаются историей; upstream_failed-таск не ретраится
      (не исполнялся — ретраить его упавшую зависимость). DeleteRun
      retention'а не трогает активный (реактивированный) ран. В админке —
      кнопка с подтверждением в таблице тасков; интеграционные тесты
      (ретрай упавшего и успешного, отказ на running/upstream_failed)

Фаза 6 закрыта.

### Фаза 7 — зрелость

- [x] Параметры рана и логическая дата (решение №23): `run.params` +
      `run.logical_date`, env `LOOM_RUN_PARAMS`/`LOOM_LOGICAL_DATE`,
      SDK `Params`/`BindParams`/`LogicalDate` + `run --params` и
      `LocalParams` в локальном режиме; params в триггере админки,
      логическая дата в списке и на странице рана; интеграционные тесты
- [x] Catchup и backfill (решение №24): опция дага `loom.Catchup()`,
      наверстывание тиков в cronPass (CAS от сработавшего тика, до 10 за
      проход), регистрация/unpause не сбрасывают `next_run_at` catchup-дага;
      RPC `BackfillRun` (лимит 100 тиков за вызов, без идемпотентности) +
      модалка backfill в админке; интеграционные тесты
- [x] Значения тасков / XCom (решение №25): таблица `run_value`,
      `TaskValueService` (Push — токен attempt'а + отсечка неактуальной
      попытки, Pull — токен рана, List — админка), SDK
      `rt.PushValue`/`rt.PullValue` (grpc- и файловая реализации порта
      valueStore), секция значений на странице рана; тесты SDK и сервера
- [x] Пулы и приоритеты (решение №26): таблица `pool` (seed default/64) +
      `PoolService` (List/Set, без удаления), опции таска `Pool`/`Priority`,
      pool/priority денормализованы в `task_instance`, claim в одной
      транзакции лочит пулы и раздаёт свободные слоты приоритетным первыми;
      `max_active_runs` дага (`loom.MaxActiveRuns`) — раскрутка только N
      старейших активных ранов; существование пулов проверяется при
      регистрации; страница пулов в админке; интеграционные тесты
- [x] Секреты (решение №27): таблица `secret` (AES-256-GCM при `SECRET_KEY`),
      write-only `SecretService` (Set/List-метаданные/Delete), опция таска
      `loom.Secret(env, name)` (валидация env-имён, не LOOM_*), резолв и
      инъекция при Launch (отсутствующий секрет → launch_failed); страница
      секретов в админке; интеграционные тесты (включая шифрование в БД)
- [x] `DockerExecutor` (решение №28, `EXECUTOR=docker`): вторая реализация
      executor-интерфейса поверх docker CLI — `run -d` с лейблами и
      детерминированным именем контейнера, started сразу после запуска,
      finished поллингом `ps`/`inspect` (exit code, OOMKilled) с удалением
      контейнера, ListAlive по лейблу, лимиты cpu/mem через `--cpus`/
      `--memory`. Обкатан на живом демоне (2026-08-21): полный цикл
      demo-etl, стримовое ребро, ретраи, RetryTask, секреты, логи

Отложено по решению пользователя (2026-08-21, не делаем без явного запроса):

- Масштабирование artifact-сервера; storage-бэкенд за интерфейсом (S3/PVC)
- `local-strict` режим: таски подпроцессами (`exec` самого себя) без docker
- Уведомления о провале рана (webhook/мессенджеры, SDK-колбэки)

### Фаза 8 — эксплуатация

Состав согласован с пользователем 2026-08-21 (деплой, наблюдаемость,
auth админки, публикация SDK, CI с unit-тестами). Порядок — CI первым
(дальше всё едет под его защитой), деплой последним (собирает остальное).

- [x] CI (GitHub Actions, без базы в тестах): `.github/workflows/ci.yml` —
      матрица go по модулям (api/sdk/artifact/server/examples: `go vet` +
      `go test -race`; интеграционные `server/test` сами скипаются без
      `TEST_PG_DSN` — БД в CI не поднимаем), админка (`pnpm typecheck` +
      `pnpm lint`), job `images` (после go+admin): `make build` +
      `make build-admin` + docker-образы server/artifact — build-only на
      PR/пуше, пуш в `ghcr.io/rendau/loom/{server,artifact}` по тегу `v*`
      (semver-тег + latest; теги модулей `api/v*`, `sdk/v*` образы не
      триггерят)
- [x] Наблюдаемость — Prometheus-метрики на system-порту (`/metrics`,
      3004/3003, рядом с healthcheck): `internal/infra/metrics` (Registry +
      Factory по gotemplate, go/process-коллекторы) в обоих сервисах;
      планировщик — `loom_scheduler_*` (task_instances по нетерминальным
      статусам, pool_slots/pool_busy, pass_duration_seconds,
      cron_lag_seconds; выборка гейджей — в pass, best-effort), раны и
      попытки — `loom_run_finished_total`/`loom_run_duration_seconds`
      (только applied FinishRun — без двойного счёта при гонке) и
      `loom_attempt_finished_total{success,reason}`/
      `loom_attempt_duration_seconds` (started_at — из RETURNING
      FinalizeAttempt), executor — `loom_executor_launches_total`/
      `launch_errors_total`, artifact — `loom_artifact_active_{write,read}_
      streams`/`received_bytes_total`; стандартные `grpc_server_*` —
      интерцептор go-grpc-middleware providers/prometheus +
      InitializeMetrics. При `EXECUTOR=none` метрик планировщика нет
      (он не создаётся). Интеграционные тесты зелёные, /metrics обоих
      сервисов проверен смоуком
- [x] Auth админского API — статический bearer-токен (`ADMIN_TOKEN` на
      server; пусто — выключено, dev): интерцепторы (`app/auth.go`,
      unary+stream, constant-time сравнение) требуют `Authorization:
      Bearer` на всех RPC, кроме исключений — task-facing PushTaskLog и
      Push/PullTaskValue (attempt-токены), PushDagManifest (describe_id,
      решение №29) и reflection; новые RPC защищены по умолчанию. 401 —
      телом `ErrorRep{code: not_authorized}` через gateway (Authorization
      пробрасывается дефолтным header matcher'ом, CORS его уже разрешал).
      В админке — модалка ввода токена (`AuthTokenModal`, открывается по
      401 через флаг `authNeeded` и кнопкой в футере сайдбара), токен в
      localStorage, заголовок в api-клиенте (`authHeaders`, включая
      стриминговое чтение логов), сохранение — reload SPA. Юнит-тесты
      интерцептора; вручную проверено curl'ом (401/200/dev-режим) и в
      превью (модалка по 401). Нюанс Nuxt: auto-импортированному ref
      нельзя присваивать прямо в шаблоне — в модалке writable computed
- [x] Публикация SDK (механика — решение №15): теги `api/v0.1.0` и
      `sdk/v0.1.0` запушены (2026-08-21); `sdk/go.mod` фиксирует
      `api v0.1.0` обычным require (go mod tidy работает без go.work),
      `sdk/README.md` с минимальным дагом и опциями; инструкция релиза
      (порядок тегирования api → sdk) — в корневом README, раздел
      «Релиз SDK». Проверено из чистого модуля вне монорепы:
      `go get github.com/rendau/loom/sdk@v0.1.0` → сборка минимального
      дага, describe (sdk_version 0.1.0 в манифесте) и локальный ран
- [ ] Первый деплой и обкатка:
      - ~~Dockerfile server'а~~ — есть (бинарь + миграции + статика
        админки), CI собирает и пушит оба образа
      - ~~решить регистрацию дагов без docker-демона рядом с server'ом~~ —
        решено и реализовано (решение №29): describe одноразовым k8s
        Job'ом (`k8sdescriber` при `EXECUTOR=k8s`), docker-CLI при
        docker/none
      - ~~helm-чарт~~ — `deploy/helm/loom` (2026-08-21), по конвенциям
        helm-zeon (копируется в charts/ helm-репо): server + artifact
        одним чартом в namespace релиза, реплики = 1 (RWO PVC под
        artifact-данные и логи), RBAC Role на Jobs/pods/pods-log для
        executor'а и describe, секреты словарём (плейсхолдеры в чарте,
        реальные — в приватных values), traefik-ингрессы админки и API
        (CORS включён — хосты разные), ServiceMonitor'ы на system-порты,
        keel/reloader-аннотации; `helm lint`/`template` зелёные.
        Обновление 2026-08-21 (loom публичный, даги приватные с
        отдельными кредами): `pull_secret` самого loom опционален (пусто —
        образы публичные), а креды дагов — одним dockerconfigjson-секретом
        `server.dag_registry_secret` на оба применения: imagePullSecrets
        подов тасков/describe (`K8S_IMAGE_PULL_SECRET` → k8sexecutor и
        k8sdescriber подставляют в спеки) и digest-чек авто-обновления
        (`REGISTRY_AUTH_FILE`); обход через default SA больше не нужен
      - docker-compose для одного хоста (`EXECUTOR=docker` + Postgres +
        artifact) — не делали
      - ~~обкатка~~ — выполнена 2026-08-21 на docker-desktop (демон +
        его k8s), пометки «не гонялся» с №8, №28 и №29 сняты. Стенд:
        локальный registry (127.0.0.1:5001, digest-пиннинг настоящий),
        artifact+server на хосте, поды/контейнеры ходят на
        `host.docker.internal`, `AUTH_SECRET` включён. Прогнано в обоих
        режимах: полный цикл demo-etl (стримовое ребро ко-стартует),
        регистрация через describe-Job (№29), ретраи
        (up_for_retry → backoff → attempt 2), RetryTask подграфа,
        секрет env-инъекцией, params/logical_date, Push/PullValue,
        чтение логов (task + server-строки). Второй даг обкатки —
        внешний потребитель sdk v0.1.0 (secret+flaky+values)
      - ~~документация по развёртыванию~~ — `deploy/README.md`: установка
        чарта в helm-zeon/-test (приватные values, helmfile-релиз),
        pull-секреты для образов дагов, жизненный цикл дага — деплой и
        обновление (перерегистрация, пиннинг активных ранов)
      - остаётся: docker-compose (выше) и сам первый деплой в кластер —
        копирование чарта в helm-репо и установка за пользователем
- [x] Авто-обновление дагов (решение №30, 2026-08-21): колонка
      `dag.auto_update` (+ в init-миграции), `registrycli` (digest-чек
      Docker Registry API v2: HEAD manifests → Docker-Content-Digest,
      GET-fallback c sha256 тела, Bearer token-flow и Basic, креды —
      docker config.json `REGISTRY_AUTH_FILE`, localhost — http; юнит-
      тесты на httptest-registry), компонент `domain/dagsync`
      (`DAG_SYNC_TICK` 5m, метрики `loom_dagsync_*`; перерегистрация —
      через обычный Register-флоу usecase'а, флаг не трогает), API:
      `auto_update` в DagMain/RegisterDag (optional — не сбрасывается) +
      `SetDagAutoUpdate` (PUT /dag/{name}/auto_update); админка — чекбокс
      в регистрации, бейдж «auto» и переключатель в списке; чарт
      монтирует dockerconfigjson-секрет (`server.registry_auth_secret`).
      Проверено вживую на docker-стенде: push нового образа тем же тегом
      → авто-перерегистрация за один тик → новый ран исполнился на новой
      версии (`done v2`), активные раны не тронуты. Дополнение (даги
      приватные): `K8S_IMAGE_PULL_SECRET` — имя dockerconfigjson-секрета,
      который k8sexecutor/k8sdescriber подставляют в imagePullSecrets
      подов попыток и describe-Job'ов (пусто — без секрета); в чарте оба
      механизма включает одно значение `server.dag_registry_secret`

## Команды

```bash
make generate-proto   # protoc: api/proto → api/common, api/artifact_v1, api/server_v1 (+gateway)
make build            # artifact и server → <модуль>/cmd/build/svc
make build-admin      # SPA админки: pnpm generate → server/admin-ui (ADMIN_DIR)
make test             # тесты sdk, artifact и server (в CI гонять с -race);
                      # интеграционные server/test требуют TEST_PG_DSN=postgres://...
cd examples && go run ./demo-etl run   # локальный прогон примера

# распределённый запуск таска против локального artifact-сервера
# (AUTH_SECRET не задан — токены выключены, для прода задать одинаковый
# секрет на server и artifact):
DATA_DIR=/tmp/loom-data ./artifact/cmd/build/svc &
cd examples && go build -o /tmp/demo-etl ./demo-etl
LOOM_ARTIFACT_ADDR=127.0.0.1:5051 LOOM_RUN_ID=r1 /tmp/demo-etl run --task=extract

# control plane (нужен Postgres; EXECUTOR=k8s|docker|none):
#   docker — контейнеры тасков на хосте (TASK_*_ADDR, напр.
#   host.docker.internal:5051); none — dev без запуска тасков
cd server && PG_DSN=postgres://... EXECUTOR=none ./cmd/build/svc
# REST (gateway): POST /dag {image}, POST /run {dag_name, params?},
#                 POST /run/backfill {dag_name, from, to}, GET /run/{id},
#                 GET /run/{id}/value, PUT /pool/{name}, PUT /secret/{name},
#                 GET /run/{id}/task/{t}/attempt/{n}/log?follow=true
```
