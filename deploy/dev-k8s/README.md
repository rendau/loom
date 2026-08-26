# Dev-стенд в kubernetes (docker-desktop)

Локальный стенд для проверки **k8s-режима**: таски и `describe` запускаются
настоящими Job'ами, а не docker-контейнерами. Прод-деплой живёт в отдельной
репе с чартами — это только для разработки.

Всё в namespace `loom`: Postgres, registry образов дагов, artifact и server
(он же раздаёт админку). Данные — в PVC, поэтому стенд переживает
перезапуск подов и паузу между сессиями.

Контекст указывается **явно**: дефолтный может смотреть на боевой кластер.

## Поднять

```bash
make build-admin
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o cmd/build/svc cmd/main.go
cd ../artifact && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o cmd/build/svc cmd/main.go
cd .. && docker build -t loom-server:dev server/ && docker build -t loom-artifact:dev artifact/
kubectl --context docker-desktop apply -f deploy/dev-k8s/stand.yaml
```

Образы не пушатся: runtime у docker-desktop — docker, поэтому локальные
образы видны кластеру (`imagePullPolicy: IfNotPresent`). После пересборки —
`kubectl --context docker-desktop -n loom rollout restart deploy/server`.

## Образ дага

describe-Job всегда пуллит образ (`imagePullPolicy: Always`), поэтому даг
должен лежать в настоящем registry. Registry стенда торчит NodePort'ом
30500: этот адрес одинаково доступен с хоста (push) и с узла (pull
kubelet'ом), поэтому образ так и называется.

```bash
cd examples && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/multi-dag ./multi-dag
# Dockerfile: FROM alpine:3 + COPY multi-dag + ENTRYPOINT
docker build -t localhost:30500/multi-dag:latest -f <dockerfile> /tmp
docker push localhost:30500/multi-dag:latest
```

Регистрировать проект в админке с образом `localhost:30500/multi-dag:latest`.

## Доступ

```bash
kubectl --context docker-desktop -n loom port-forward svc/server 8081:8081 8082:8082
```

- админка — http://localhost:8081
- gateway API — http://localhost:8082 (`ADMIN_API_BASE_URL` в манифесте
  указывает именно на него)

Первый администратор создаётся на `/setup` в самой админке.

## Чего на этом стенде не увидеть

- **Размер образа проекта** остаётся `0`: server резолвит его по registry
  API, а `localhost:30500` внутри его пода — это сам под. Чтобы адрес
  резолвился и из пода, и с узла, нужен registry с общим именем (ingress
  или hostAlias) — для локальной проверки не стоит возни.
- **Digest** пиннится из `imageID` пода, который сообщает kubelet. Если
  образ в локальном docker имеет несколько repo digest'ов (например, его
  тегировали и в `127.0.0.1:5001`, и в `localhost:30500`), в записи проекта
  может оказаться любой из них.
- **Пик памяти попыток** не собирается — нет metrics-server.

## Диагностика

```bash
kubectl --context docker-desktop -n loom get pods,jobs
kubectl --context docker-desktop -n loom logs deploy/server --tail=50
kubectl --context docker-desktop -n loom logs job/<job-name>
```

Семплинг памяти попыток выключен (`K8S_METRICS_TICK=0`): в docker-desktop
нет metrics-server, иначе в логах копились бы ошибки metrics.k8s.io.

## Погасить / удалить

```bash
kubectl --context docker-desktop -n loom scale deploy --all --replicas=0   # пауза, данные целы
kubectl --context docker-desktop delete namespace loom                      # снести совсем (с PVC)
```
