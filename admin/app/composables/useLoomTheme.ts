// Цветовая тема инсталляции: акцент + затонированная под него нейтральная
// шкала (фон, карточки, границы). Сами палитры — в assets/css/main.css,
// селекторы :root[data-loom-theme='…']; здесь только выбор и его хранение.
// Тема — предпочтение конкретного браузера, поэтому localStorage, а не БД.
export const LOOM_THEME_STORAGE_KEY = 'loom-theme'

export const LOOM_THEMES = [
  { value: 'emerald', label: 'Emerald Carbon', description: 'тёмно-зелёный, изумрудный акцент' },
  { value: 'indigo', label: 'Indigo Obsidian', description: 'почти чёрный, индиго-акцент' },
  { value: 'amber', label: 'Amber Ember', description: 'тёплый угольный, янтарный акцент' },
] as const

export type LoomTheme = (typeof LOOM_THEMES)[number]['value']

export const DEFAULT_LOOM_THEME: LoomTheme = 'emerald'

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
