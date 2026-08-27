// Цветовая тема инсталляции: акцент + затонированная под него нейтральная
// шкала (фон, карточки, границы). Сами палитры — в assets/css/main.css,
// селекторы :root[data-loom-theme='…']; здесь только выбор и его хранение.
// Тема — предпочтение конкретного браузера, поэтому localStorage, а не БД.
export const LOOM_THEME_STORAGE_KEY = 'loom-theme'

export const LOOM_THEMES = [
  { value: 'classic', label: 'Slate Classic', description: 'прежний стиль: сланцевый фон, зелёный акцент' },
  { value: 'naive', label: 'Naive Mint', description: 'почти чёрный фон, мятный акцент — как в Naive UI' },
] as const

export type LoomTheme = (typeof LOOM_THEMES)[number]['value']

// Дефолт — прежний вид админки: тот, кто тему не выбирал, ничего не теряет.
// Убранная тема в localStorage не ломает загрузку: isLoomTheme её отсеет и
// плагин вернётся к дефолту.
export const DEFAULT_LOOM_THEME: LoomTheme = 'classic'

export function isLoomTheme(v: unknown): v is LoomTheme {
  return LOOM_THEMES.some(t => t.value === v)
}

// Применяется и плагином до монтирования приложения (loom-theme.client),
// и переключателем — отсюда отдельная функция.
export function applyLoomTheme(theme: LoomTheme) {
  document.documentElement.dataset.loomTheme = theme
}

export function useLoomTheme() {
  const theme = useState<LoomTheme>('loom-theme', () => DEFAULT_LOOM_THEME)

  function setTheme(next: LoomTheme) {
    theme.value = next
    applyLoomTheme(next)
    localStorage.setItem(LOOM_THEME_STORAGE_KEY, next)
  }

  return { theme, setTheme, themes: LOOM_THEMES }
}
