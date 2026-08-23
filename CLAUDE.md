# loom

Оркестратор пайплайнов (аналог Apache Airflow) с дагами на Go: один
docker-образ = один даг, таски запускаются экземплярами того же образа,
данные между тасками текут персистентными стримами через artifact-сервер.

## Модули (go.work)

- `sdk/` — публичная либа для дагов (пакет `loom`); внутри `sdk/streamstore/` —
  общая стейт-машина файловых стримов (единственная реализация, используют и
  artifact-сервер, и локальный режим). Все gRPC-подключения SDK — через общие
  dial-опции (`grpc_dial.go`): keepalive для быстрого обнаружения мёртвого
  соединения, агрессивный reconnect-backoff, ретрай unary на UNAVAILABLE
- `api/` — proto-контракты (`api/proto/`) и сгенерированный код (коммитится)
- `artifact/` — data plane: артефакты **и логи тасков** (отдельные
  streamstore-каталоги: `DATA_DIR` и `LOG_DIR` — у лог-стримов свой
  жизненный цикл), каркас по gotemplate
- `server/` — control plane, **stateless** (без дискового состояния):
  Postgres (mobone) + gRPC/gateway, регистрация дагов через `describe`
  (docker-CLI, а при `EXECUTOR=k8s` — одноразовый k8s Job, `k8sdescriber`),
  планировщик (очередь `FOR UPDATE SKIP LOCKED`, чистый planner +
  executor-порт, cron-триггер по `next_run_at` с catchup-режимом, ретраи
  `up_for_retry` с backoff, таймаут-watchdog, зомби-reconcile), executor'ы
  k8s и docker (`EXECUTOR=k8s|docker|none`), retention (`RUN_TTL`), ретрай
  таска/подграфа (`RetryTask`, только на завершённом ране), параметры рана
  и `logical_date`, backfill, значения тасков (XCom, `run_value`), пулы
  слотов с приоритетами и `max_active_runs`, секреты с env-инъекцией
  (`SECRET_KEY`). Логи не хранит — финализирует (`FinishTaskLog`) и
  проксирует чтение (`ReadTaskLog`) с artifact-сервера
- `examples/` — примеры дагов
- `admin/` — админка: Nuxt 4 SPA (`ssr: false`) + Nuxt UI v4 (НЕ Naive UI;
  образец — проект caravaneer). Раздаётся server'ом на `ADMIN_PORT` (8081)
  из `ADMIN_DIR` (`make build-admin`), рантайм-конфиг — `/config.js` из env
  `ADMIN_API_BASE_URL`; дев — `pnpm dev` + `.env` (+ `HTTP_CORS=true` на
  gateway). После правок: `pnpm typecheck` → `pnpm lint`. См. `admin/README.md`

## Ключевая семантика (кратко)

- Артефакт скоупится на попытку `(run_id, task, attempt, name)`;
  `writing → committed | aborted`; follow-читатели с tail-follow.
- Commit выходов — по успеху таска (`return nil`), abort — по ошибке/панике.
  `Close()` выхода опционален и НЕ коммитит. Commit подтверждается
  artifact-сервером: успешный return таска гарантирует данные на диске.
- `After` — ждать успеха отправителя; `AfterStreamed` — ко-старт и чтение по
  мере записи (opt-in).
- Локальный режим: `<dag-binary> run [--params='{...}']`, артефакты и
  значения в `.loom/runs/<run-id>/`, остаются после рана.
- Мелкие значения тасков — `rt.PushValue`/`rt.PullValue` (через control
  plane, лимит 64KB); параметры рана — `rt.Params()`/`rt.BindParams()`,
  «дата данных» — `rt.LogicalDate()`.
- Распределённый режим: `run --task=<name>` + env-контракт `LOOM_*`;
  программный вход — `DAG.RunTask`. Внутрикластерные RPC (SDK ↔ servers) не
  аутентифицируются (токенов нет — осознанное решение); админские RPC
  control plane закрыты `ADMIN_TOKEN`.
- Логи тасков: SDK → artifact-сервер (`artifact_v1.TaskLogService`,
  bidi-стрим с seq/ack) — **без потерь и дублей**, обрыв переживается
  реконнектом с досылкой неподтверждённого хвоста; дубль каждой строки в
  честный stdout (fd 1/2 перехватываются dup2) — только страховка на случай
  смерти процесса, основной канал — стрим. Commit лог-стрима делает
  планировщик (`FinishTaskLog`, дописывает исход попытки).
- Реконнекты — во всех соединениях; рестарт artifact/server не роняет таски:
  - лог-синк: буфер неподтверждённых строк, seq-дедупликация на сервере;
  - писатель артефактов: bidi с байтовыми ack'ами и resume — обрыв стрима
    без commit/abort НЕ абортит запись (streamstore: `Release`/`ResumeWrite`,
    судьбу брошенного стрима решает `FinishAttempt`);
  - follow-читатель артефактов: переоткрытие со своего offset'а;
  - unary (values, finish, stat): gRPC retry policy;
  - полный буфер (32k строк лога / 4MiB записи) блокирует таск
    (backpressure) — данные не дропаются, пока попытка жива.

## Деплой

Helm-чарты живут в отдельной репе — в этой их нет и на них не оглядываемся.
CI (`.github/workflows/ci.yml`) пересобирает и пушит образ только при
изменении content-хэша его входов (git-блобы модуля + общие `api/`/`sdk/`,
для server ещё `admin/`): digest тега `latest` без изменений не меняется, и
деплой, следящий за digest'ом, не рестартит нетронутые поды.

## Проверка

```bash
make test    # + в сомнительных случаях: go test -race ./... по модулям
make build
cd examples && go run ./demo-etl run
```

Интеграционные тесты `server/test` требуют Postgres: `TEST_PG_DSN=postgres://...`
(без него — skip). Одноразовую БД поднимать **только через docker** (не через
Postgres.app/initdb):

```bash
docker run --rm -d --name loom-test-pg -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=loom_test -p 54329:5432 postgres:17
TEST_PG_DSN='postgres://postgres:postgres@127.0.0.1:54329/loom_test?sslmode=disable' make test
docker stop loom-test-pg
```

Рестарт-устойчивость покрыта тестами: `artifact/test` (реконнект лог-синка и
стримов артефактов через рестарт сервера), `sdk/streamstore` (resume),
`artifact/internal/domain/tasklog` (seq-дедупликация, recovery).

Если docker-демон не запущен — попросить пользователя запустить, а не
импровизировать с локальными инсталляциями Postgres.

После правок proto — `make generate-proto` (сгенерированное коммитим).
