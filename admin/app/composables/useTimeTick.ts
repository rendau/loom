// Общий минутный тик «сейчас» для относительного времени: один интервал
// на всё приложение, чтобы «N мин назад» в списках не застывало между
// поллингами. SPA — модульный таймер живёт всё время сессии.
let timer: ReturnType<typeof setInterval> | undefined

export function useTimeTick() {
  const now = useState('time-now', () => Date.now())
  if (import.meta.client && !timer) {
    timer = setInterval(() => {
      now.value = Date.now()
    }, 30_000)
  }
  return now
}
