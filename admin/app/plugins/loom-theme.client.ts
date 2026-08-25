// Восстановление выбранной цветовой темы до монтирования приложения:
// атрибут на <html> ставится раньше первого рендера, поэтому переключения
// палитры на глазах у пользователя не видно.
export default defineNuxtPlugin({
  enforce: 'pre',
  setup() {
    const { theme } = useLoomTheme()

    const stored = localStorage.getItem(LOOM_THEME_STORAGE_KEY)
    theme.value = isLoomTheme(stored) ? stored : DEFAULT_LOOM_THEME
    applyLoomTheme(theme.value)
  },
})
