# Развёртывание loom

## Helm-чарт (`deploy/helm/loom`)

Чарт разворачивает оба plane'а в namespace релиза:

- **server** (control plane): gRPC `5052`, HTTP API gateway `8082`, админка
  `8081`, system `3004`. Executor — `k8s` (in-cluster): Job'ы тасков и
  describe-Job'ы регистрации создаются в том же namespace (RBAC — Role в
  namespace релиза). Логи тасков — на PVC `loom-logs`.
- **artifact** (data plane): gRPC `5051`, system `3003`, данные на PVC
  `loom-artifact-data`.

Реплики фиксированы = 1 (RWO-тома). Postgres чарт **не** разворачивает —
DSN приходит секретом.

Наружу (traefik + cert-manager, по конвенциям helm-zeon) публикуются два
хоста: `server.domain_admin` — SPA админки, `server.domain_api` — HTTP API
(нужен браузеру админки; защищён `ADMIN_TOKEN`, CORS включён). gRPC наружу
не публикуется.

### Установка в helm-zeon / helm-zeon-test

1. Скопировать `deploy/helm/loom` → `charts/loom` helm-репозитория.
2. Приватные значения — `values/loom.yaml` (папка вне git):

   ```yaml
   server:
     domain_admin: loom.<домен>
     domain_api: loom-api.<домен>

   secrets:
     PG_DSN: 'postgres://user:pass@host:5432/loom'
     AUTH_SECRET: '<random 32+ bytes>'   # одинаков на server и artifact (чарт сам разводит)
     SECRET_KEY: '<парольная фраза шифрования секретов дагов>'
     ADMIN_TOKEN: '<bearer-токен админки>'
   ```

3. Релиз в `helmfile.yaml` (namespace выделенный — в нём же будут крутиться
   поды тасков):

   ```yaml
   - name: loom
     chart: charts/loom
     namespace: loom
     installed: true
     labels:
       rel: loom
     values:
       - values/loom.yaml
   ```

4. Миграции БД server применяет сам при старте (`migrations/` в образе).

### Образы дагов из приватного registry

Сам loom публичный (образы `ghcr.io/rendau/loom/*` тянутся без кредов,
`pull_secret` в values пустой), а вот **образы дагов приватные** и их
креды — отдельные, не креды loom. Один dockerconfigjson-секрет в
namespace loom закрывает всё:

```bash
kubectl -n loom create secret docker-registry loom-dag-registry \
  --docker-server=ghcr.io \
  --docker-username=<user> \
  --docker-password=<PAT c read:packages>
```

и в приватных values:

```yaml
server:
  dag_registry_secret: loom-dag-registry
```

Чарт использует его двумя путями:

- имя уходит в `K8S_IMAGE_PULL_SECRET` — loom подставляет
  `imagePullSecrets` в поды тасков и describe-Job'ы (pull приватного
  образа kubelet'ом);
- содержимое монтируется как `REGISTRY_AUTH_FILE` (docker `config.json`) —
  digest-чек авто-обновления (решение №30) ходит в приватный registry.

Несколько registry — обычный `config.json` с несколькими `auths`
поддерживается; но `imagePullSecrets` ссылается на один секрет, поэтому
держите все креды дагов в одном dockerconfigjson-секрете.

## Жизненный цикл дага: деплой и обновление

Даг — это docker-образ (один образ = один даг). Регистрация и обновление —
одна и та же операция:

```
POST /dag {"image": "<registry>/<dag>:<tag>"}     # или кнопка
                                                  # «Зарегистрировать» в админке
```

При регистрации control plane поднимает одноразовый **describe-Job** с этим
образом (решение №29), получает от него манифест (таски, рёбра, расписание,
ретраи, пулы, секреты), валидирует и сохраняет вместе с **digest'ом** образа.

**Обновление запущенного дага** — шаги:

1. Собрать новый образ дага и запушить в registry (тег может быть тем же,
   например `:latest` — digest резолвится заново при регистрации).
2. Перерегистрировать: `POST /dag` с тем же именем образа (админка — та же
   кнопка). Имя дага берётся из манифеста — запись обновится, не
   задублируется.
3. Готово. Дальше само:
   - **активные раны не затрагиваются**: ран пиннится к digest'у образа и
     снапшоту манифеста на момент триггера — бегущие и уже запланированные
     внутри рана таски доработают на старой версии;
   - **новые раны** (cron, ручные, backfill) пойдут на новом digest'е и
     новом манифесте;
   - **RetryTask старого рана** тоже выполняется на его старом digest'е —
     ретрай воспроизводим;
   - **расписание**: у обычного дага `next_run_at` пересчитывается от
     «сейчас» (решение №17); у catchup-дага — сохраняется, пропущенные тики
     довыполнятся (решение №24).

Важно: просто запушить новый образ сам по себе ничего не меняет — digest
резолвится в момент регистрации. Дальше два способа подхвата:

**Авто-обновление (аналог keel poll, решение №30).** У дага есть флаг
`auto_update` (галка при регистрации/в списке дагов админки, либо
`POST /dag {"image": ..., "auto_update": true}`). Control plane раз в
`DAG_SYNC_TICK` (дефолт 5m) сверяет digest тега в registry (дешёвый HEAD,
без скачивания образа) и при изменении перерегистрирует даг сам — шаг 2
выше не нужен, достаточно запушить образ. Нюансы:

- креды к приватному registry — dockerconfigjson-секрет, чарт монтирует
  его в под server (`server.registry_auth_secret`, дефолт `ghcr.io`);
- сломанный новый образ (упал describe, невалидный манифест) запись дага
  не трогает: ошибка в логе/метрике (`loom_dagsync_*`), даг живёт на
  старой версии до следующего тика;
- работает только для дагов, зарегистрированных тегом и пиннутых
  digest'ом; ref вида `repo@sha256:...` не синкается (обновлять нечего);
- перерегистрация (ручная и авто) флаг `auto_update` не сбрасывает.

**Через CI дага (точный контроль момента).** Шаг 2 из CI:

```bash
curl -X POST https://loom-api.<домен>/dag \
  -H "Authorization: Bearer $LOOM_ADMIN_TOKEN" \
  -d '{"image":"ghcr.io/<org>/<dag>:latest"}'
```

Снять даг с расписания без удаления — pause (`PUT /dag/{name}/paused`,
в админке переключатель); удаление дага не трогает историю ранов.

## Что ещё не покрыто чартом

- docker-compose для одного хоста (`EXECUTOR=docker`) — отдельный пункт
  фазы 8, чартом не решается.
