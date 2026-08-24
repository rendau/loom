// debounceFn — простейший debounce для фильтров-инпутов (vueuse в
// зависимостях нет, тянуть его ради одной функции не стоит).
export function debounceFn<A extends unknown[]>(fn: (...args: A) => void, delay = 300) {
  let timer: ReturnType<typeof setTimeout> | undefined
  return (...args: A) => {
    clearTimeout(timer)
    timer = setTimeout(() => fn(...args), delay)
  }
}
