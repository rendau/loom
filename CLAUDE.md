# loom

Оркестратор пайплайнов (аналог Apache Airflow) с дагами на Go: один
docker-образ = один даг, таски запускаются экземплярами того же образа,
данные между тасками текут персистентными стримами через artifact-сервер.

**План, чеклист и зафиксированные архитектурные решения — в [ROADMAP.md](ROADMAP.md).
Начинай с него: решения из раздела «Решения» не пересматриваем без явного
запроса пользователя, выполненные пункты чеклиста отмечаем.**

## Модули (go.work)

- `sdk/` — публичная либа для дагов (пакет `loom`); внутри `sdk/streamstore/` —
  общая стейт-машина файловых стримов (единственная реализация, используют и
  сервер, и локальный режим)
- `api/` — proto-контракты (`api/proto/`) и сгенерированный код (коммитится)
- `artifact/` — artifact-сервер (data plane), каркас по gotemplate
- `server/` — control plane: Postgres (mobone) + gRPC/gateway, регистрация
  дагов через docker `describe`, планировщик (очередь `FOR UPDATE SKIP
  LOCKED`, чистый planner + executor-порт, cron-триггер по `next_run_at`,
  ретраи `up_for_retry` с backoff, таймаут-watchdog, зомби-reconcile),
  k8s-executor, приём/чтение логов (streamstore), retention (`RUN_TTL`),
  attempt-токены (`AUTH_SECRET`, пакет `api/attempttoken`)
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
  `Close()` выхода опционален и НЕ коммитит.
- `After` — ждать успеха отправителя; `AfterStreamed` — ко-старт и чтение по
  мере записи (opt-in).
- Локальный режим: `<dag-binary> run`, артефакты в `.loom/runs/<run-id>/`,
  остаются после рана.
- Распределённый режим: `run --task=<name>` + env-контракт `LOOM_*`;
  программный вход — `DAG.RunTask`. Логи — батчами на control plane
  (`server_v1.TaskLogService`) с дублем в честный stdout; fd 1/2 процесса
  перехватываются (dup2).

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

Если docker-демон не запущен — попросить пользователя запустить, а не
импровизировать с локальными инсталляциями Postgres.

После правок proto — `make generate-proto` (сгенерированное коммитим).
