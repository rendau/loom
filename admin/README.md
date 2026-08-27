# loom admin

Админка loom: Nuxt 4 SPA (`ssr: false`) + Nuxt UI v4 поверх gateway API
control plane (grpc-gateway, порт 8082). Обзор («что требует внимания»),
проекты (docker-образы: регистрация, каталог дагов образа, «завести даг» от
шаблона, обновление из registry; страница шаблона
`/projects/<проект>/templates/<шаблон>` — граф, таски и требования к
окружению, общие для всех инстансов), даги (`/dags/<проект>/<даг>`; карточка:
табы Обзор/Схема/Env/Настройки — раны и метрики, граф и манифест,
требования к окружению),
раны (страница рана — master-detail: список тасков + инспектор с табами
Лог/Попытки/Значения/Env, `?task=&tab=` в URL), live-логи (follow, разбор
logfmt/JSON/ANSI), переменные и секреты одним разделом (`/env`) с тремя
скоупами (глобальный, проекта, дага), пользователи (назначения — на проекты
и точечно на даги). Дизайн-документы редизайна — `docs/design/`
(вьюпорт-бюджет 1180px, плотные таблицы, относительное время в списках).

В шапке каждой карточки — путь от раздела до открытой сущности
(`Даги › demo › nsi_technodom`) и бейдж её типа: идентификаторы составные и
похожи друг на друга («demo/nsi_sync» — и шаблон, и даг). Раздел в сайдбаре
остаётся подсвеченным на всех вложенных страницах, а не только на списке.

Вход — по логину и паролю (сессия хранится в localStorage). Пока в системе
нет ни одного пользователя, админка открывает экран первичной настройки
(`/setup`) — там создаётся первый администратор.

## Разработка

```sh
pnpm install
cp .env.example .env   # NUXT_PUBLIC_API_BASE_URL=http://localhost:8082
pnpm dev               # http://localhost:3000
```

Gateway при этом должен быть запущен с `HTTP_CORS=true` (dev-режим сервера:
`EXECUTOR=none`).

Проверки: `pnpm typecheck` и `pnpm lint`.

## Переменные дага: что он требует от окружения

Состав переменных и секретов даг объявляет в коде (`loom.Variable` /
`loom.Secret`, опциональное третье поле — описание) и приносит манифестом
при регистрации; значения заводятся в админке. Таба «Env» карточки дага
показывает их вместе: описание из кода, env-имя в контейнере, какие таски
используют, заполнено ли значение и в каком скоупе — и позволяет заполнить
на месте (глобально, на проект или на этот даг). Незаполненные считаются по тому
же правилу, что при `launch`, и выносятся бейджем в список дагов и строкой
в «требует внимания» на обзоре — иначе о разрыве узнают только по
`launch_failed` первого запуска.

Резолв — клиентский (`utils/dagenv.ts` + `useDagEnvRequirements` /
`useDagEnvGaps`), отдельного RPC нет: манифест и записи и так приходят
в админку.

## Цветовые темы

Две темы — `classic` (по умолчанию — прежний вид админки: сланцевый фон,
зелёный акцент) и `naive` (почти чёрный фон с холодным отливом, мятный
акцент и уменьшенный радиус — палитра тёмной темы Naive UI). Тема = акцент
(`primary`) + затонированная под него нейтральная шкала, из которой Nuxt UI
берёт фон, карточки, границы и текст; семантика статусов
(success/error/warning/info) от темы не зависит.

Палитры — в `app/assets/css/main.css` (селекторы
`:root[data-loom-theme='…']`, переопределяют те же переменные
`--ui-color-primary-*` / `--ui-color-neutral-*`, что Nuxt UI генерит из
app.config), выбор — `useLoomTheme` (localStorage, предпочтение браузера),
переключатель — кнопка-палитра в подвале сайдбара. Смена темы работает без
пересборки; `plugins/loom-theme.client.ts` ставит атрибут на `<html>` до
первого рендера, поэтому вспышки старой палитры нет.

## Прод: раздача от control plane server

`make build-admin` (в корне репо) собирает статику (`pnpm generate` →
`.output/public`) и кладёт её в `server/admin-ui` — дефолтный `ADMIN_DIR`
сервера; `server/Dockerfile` копирует её в образ. Server раздаёт SPA на
отдельном порту `ADMIN_PORT` (дефолт 8081) с fallback на `index.html`.

## Рантайм-конфиг

Один билд на все окружения: server отдаёт `/config.js`
(`window.__APP_CONFIG__`) из своих env — `ADMIN_API_BASE_URL` (базовый URL
gateway, каким его видит браузер). В деве `/config.js` — заглушка из
`public/`, значения берутся из `.env` (`NUXT_PUBLIC_*`). Чтение — только
через хелперы `app/utils/config.ts`.

## Структура

```
app/
  api/         # тонкие типизированные вызовы gateway (client.ts — flattenQuery)
  types/       # DTO (зеркало api/proto/server_v1)
  composables/ # useApiAction (loading + тосты), useAuth (сессия и права),
               # usePolling (тик + пауза на скрытой вкладке), useTimeTick,
               # useLoomTheme (выбор цветовой темы), useDagEnvRequirements
               # и useDagEnvGaps (незаполненные переменные дагов)
  plugins/     # loom-theme.client — тема на <html> до первого рендера
  utils/       # config (рантайм-конфиг), format (даты/relative/байты), status
               # (цвет+подпись+иконка), table (denseTableUi), runenv (резолв
               # env-привязок рана), dagenv (требования дага к окружению),
               # logparse (logfmt/JSON/ANSI), auth (токен)
  middleware/  # auth.global — гард сессии (/login, /setup)
  layouts/     # default (UDashboardGroup/Sidebar), auth (вход без сайдбара)
  pages/       # / (обзор), /dags, /dags/[name] (?tab=), /runs,
               # /runs/[id] (?task=&tab=), /runs/[id]/log, /env, /pools, /users
  components/  # StatusBadge/RelativeTime/RowMenu/EmptyState/SectionHeader/
               # MetaGrid|Item — общие примитивы (docs/design/06);
               # dag/* — модалки дага + RunSpark; run/* — LogViewer,
               # TaskInspector, DagGraph, EnvTable; dashboard/* — SVG-графики
```
