// SPA админки loom. SSR отключён осознанно: собранная статика (nuxt
// generate → .output/public) раздаётся control plane server'ом на отдельном
// порту (ADMIN_PORT/ADMIN_DIR), рантайм-конфиг инжектится через /config.js
// (window.__APP_CONFIG__) — env задаются после билда, один билд на все
// окружения. Дев-режим: `pnpm dev` + .env (NUXT_PUBLIC_API_BASE_URL).
export default defineNuxtConfig({
  ssr: false,

  modules: ['@nuxt/ui', '@nuxt/eslint'],

  css: ['~/assets/css/main.css'],

  app: {
    head: {
      title: 'loom — админка',
      htmlAttrs: { lang: 'ru' },
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
      // Рантайм-конфиг: в проде /config.js генерит control plane server из
      // env; в деве отдаётся заглушка public/config.js (пустой объект).
      script: [{ src: '/config.js' }],
    },
  },

  runtimeConfig: {
    public: {
      // Дев-значение (.env → NUXT_PUBLIC_API_BASE_URL); в проде
      // перекрывается window.__APP_CONFIG__ (см. app/utils/config.ts)
      apiBaseUrl: '',
    },
  },

  typescript: {
    strict: true,
    typeCheck: false, // проверка через `pnpm typecheck` (vue-tsc), не в дев-сборке
  },

  devtools: { enabled: true },
  compatibilityDate: '2026-01-01',
})
