// Единый поллинг страниц (design/05: Обзор и списки 10–30с, живой ран 3с):
// тик по интервалу, пауза на скрытой вкладке, опциональный предикат
// enabled («пока ран running»). Колбэк вызывать фоновым рефрешем — без
// спиннера, чтобы интерфейс не мигал.
export function usePolling(cb: () => unknown, intervalMs: number, enabled?: () => boolean) {
  let timer: ReturnType<typeof setInterval> | undefined

  function tick() {
    if (document.hidden)
      return
    if (enabled && !enabled())
      return
    cb()
  }

  onMounted(() => {
    timer = setInterval(tick, intervalMs)
  })
  onBeforeUnmount(() => clearInterval(timer))
}
