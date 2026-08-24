# loom admin

Админка loom: Nuxt 4 SPA (`ssr: false`) + Nuxt UI v4 поверх gateway API
control plane (grpc-gateway, порт 8082). Дашборд, даги (карточка со схемой
до первого запуска), раны с метриками, live-логи (follow, разбор
logfmt/JSON/ANSI), переменные и секреты одним разделом (`/env`) со
скоупами, пользователи.

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
  composables/ # useApiAction (loading + тосты), useAuth (сессия и права)
  utils/       # config (рантайм-конфиг), format (даты, байты), status,
               # logparse (logfmt/JSON/ANSI), auth (токен сессии)
  middleware/  # auth.global — гард сессии (/login, /setup)
  layouts/     # default (UDashboardGroup/Sidebar), auth (вход без сайдбара)
  pages/       # / (дашборд), /dags, /dags/[name], /runs, /runs/[id],
               # /runs/[id]/log, /env (переменные и секреты), /pools, /users
  components/  # dag/* — модалки дага; run/LogViewer — просмотр лога;
               # dashboard/* — карточки и SVG-графики
```
