# Dev-стенд

Инфраструктура (Postgres, registry образов дагов) живёт в **kubernetes
docker-desktop**, а сами процессы loom — **нативно на хосте**: их
приходится пересобирать по десять раз за сессию, и `go run` для этого
быстрее любого образа. Таски запускаются docker-контейнерами на хосте
(`EXECUTOR=docker`).

Прод-деплой живёт в отдельной репе с чартами — это только для разработки.

## Порты и доступы (не менять без нужды)

| что | адрес |
|---|---|
| Postgres (k8s NodePort) | `localhost:30432`, `postgres/postgres`, базы `loom` и `loom_test` |
| Registry образов дагов (k8s NodePort) | `localhost:30500` |
| artifact — gRPC | `127.0.0.1:5051` (system http — 3004) |
| server — gRPC | `127.0.0.1:5052` |
| server — gateway API | `http://localhost:8082` (system http — 3003) |
| админка (`pnpm dev`) | `http://localhost:3000` |
| вход в админку | `admin` / `admin12345` |

Порты зафиксированы: env-файлы, `.env` админки и ссылки в заметках
рассчитаны на них.

## Поднять

```bash
kubectl --context docker-desktop apply -f deploy/dev-stand/infra-k8s.yaml
```

Контекст указывается **явно**: дефолтный может смотреть на боевой кластер.

Дальше три процесса, каждый в своём терминале (или в фоне):

```bash
cd artifact && set -a && . ../deploy/dev-stand/artifact.env && set +a && go run ./cmd/main.go
cd server   && set -a && . ../deploy/dev-stand/server.env   && set +a && go run ./cmd/main.go
cd admin    && pnpm dev
```

Первый администратор создаётся на `/setup` в самой админке (логин и пароль
— из таблицы выше).

## Образ дага

```bash
cd examples && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/build/multi-dag ./multi-dag
printf 'FROM alpine:3\nCOPY multi-dag /usr/local/bin/multi-dag\nENTRYPOINT ["/usr/local/bin/multi-dag"]\n' > /tmp/build/Dockerfile
docker build -t localhost:30500/multi-dag:latest /tmp/build
docker push localhost:30500/multi-dag:latest
```

Регистрировать проект с образом `localhost:30500/multi-dag:latest`. Один и
тот же адрес работает и для `docker push` с хоста, и для pull'а тасков, и
для резолва размера образа сервером.

Не держите образ дага под двумя именами (`docker tag` в другой registry):
digest пиннится из первого `RepoDigests`, и в проект может попасть адрес
несуществующего registry.

## Интеграционные тесты

Отдельный контейнер поднимать не нужно — база `loom_test` в том же
Postgres:

```bash
TEST_PG_DSN='postgres://postgres:postgres@127.0.0.1:30432/loom_test?sslmode=disable' make test
```

## Погасить / удалить

Нативные процессы — Ctrl+C. Инфраструктура:

```bash
kubectl --context docker-desktop -n loom scale deploy --all --replicas=0   # пауза, данные в PVC целы
kubectl --context docker-desktop delete namespace loom                      # снести совсем
```

## k8s-executor

Если нужно проверить именно k8s-режим (таски и `describe` — настоящими
Job'ами), server придётся запускать в кластере: под тасков не видит
`host.docker.internal`, поэтому нативный control plane с `EXECUTOR=k8s` для
тасков не годится. Тогда server и artifact деплоятся как Deployment'ы в тот
же namespace, а адреса в `TASK_*_ADDR` меняются на cluster DNS
(`artifact.loom.svc:5051`, `server.loom.svc:5052`) — этот путь проверялся,
и на нём, кстати, всплыл баг с аутентификацией `PushDagCatalog`.
Ограничения такого варианта: размер образа остаётся `0` (registry по
`localhost:30500` не резолвится изнутри пода) и нет metrics-server, так что
пик памяти попыток не собирается.
