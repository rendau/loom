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

## Чеклист: впереди

### Фаза 3 — распределённый запуск таска (SDK remote)

- [ ] `sdk`: remote-реализация `artifactStore` — gRPC-клиент artifact-сервера
      (Write: header → chunks(~256KB) → commit; Read: follow=true, offset 0)
- [ ] Контракт env для контейнера таска: `LOOM_SERVER_ADDR`,
      `LOOM_ARTIFACT_ADDR`, `LOOM_RUN_ID`, `LOOM_TASK`, `LOOM_ATTEMPT`,
      `LOOM_DEP_ATTEMPTS` (json map таск→attempt для чтения входов), токен
- [ ] `run --task` в cli.go: собрать Runtime с remote-бэкендом, выполнить один
      таск, вызвать FinishAttempt (или это делает control plane — решить при
      реализации), корректные exit-коды
- [ ] Интеграционный тест: два процесса обмениваются стримом через
      artifact-сервер (in-process grpc или testcontainer)
- [ ] Логи: LogSink-порт в Runtime; remote-реализация — batched gRPC-стрим
      (протокол добавить в `api/`), локальная — как сейчас в stderr
- [ ] Перехват stdout/stderr процесса таска (dup2) → в LogSink + дублирование
      в настоящий stdout

### Фаза 4 — control plane server (`server/`)

- [ ] Каркас по gotemplate: Postgres, миграции (единый `000001_init` до
      первого деплоя), gRPC + gateway, system http
- [ ] Схема БД: `dag` (name, image digest, manifest, schedule, paused),
      `run` (dag, digest, status, trigger), `task_instance` / `attempt`
      (status, started/finished, exit info)
- [ ] Регистрация дага: по url образа запустить `describe`, валидировать
      манифест, сохранить (регистрация по digest)
- [ ] Ручной триггер рана + раскрутка графа: очередь тасков
      (`FOR UPDATE SKIP LOCKED`), обычные и стримовые рёбра
- [ ] Executor-интерфейс (Launch/Watch/Kill attempt'а) + `K8sExecutor`
      (Job backoffLimit=0, labels, informer watch, env-контракт из фазы 3)
- [ ] Вызов `FinishAttempt`/`AbortArtifact` на artifact-сервере при завершении
      / смерти attempt'а; фиксация причины смерти пода (exit code, OOM)
- [ ] Приём лог-стрима от SDK, хранение (файлы через streamstore или отдельное
      хранилище — решить), API чтения логов с follow
- [ ] API для админки: список дагов/ранов, детали рана, логи

### Фаза 5 — надёжность планировщика

- [ ] Cron-расписания (без catchup в первой версии), pause/resume дага
- [ ] Ретраи с backoff (per-task политика в манифесте), таймауты таска
- [ ] Зомби-детект: heartbeat attempt'а + watch подов; переотправка в очередь
- [ ] Ресурсы таска в манифесте (cpu/mem requests/limits) → в спеку пода
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
make generate-proto   # protoc: api/proto → api/artifact_v1
make build            # artifact server → artifact/cmd/build/svc
make test             # тесты sdk и artifact (в CI гонять с -race)
cd examples && go run ./demo-etl run   # локальный прогон примера
```
