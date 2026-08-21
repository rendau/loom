# loom admin

Админка loom: Nuxt 4 SPA (`ssr: false`) + Nuxt UI v4 поверх gateway API
control plane (grpc-gateway, порт 8082). Даги, раны, статусы тасков и
попыток, live-логи (follow).

## Разработка

```sh
pnpm install
cp .env.example .env   # NUXT_PUBLIC_API_BASE_URL=http://localhost:8082
pnpm dev               # http://localhost:3000
```

Gateway при этом должен быть запущен с `HTTP_CORS=true` (dev-режим сервера:
`EXECUTOR=none`, см. корневой ROADMAP).

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
  composables/ # useApiAction (loading + тосты)
  utils/       # config (рантайм-конфиг), format (даты), status (цвета/подписи)
  layouts/     # default (UDashboardGroup/Sidebar)
  pages/       # /dags, /runs, /runs/[id]
  components/  # run/LogSlideover — просмотр лога попытки со стримом follow
```
