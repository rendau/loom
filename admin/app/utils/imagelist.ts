// Свободный ввод списка docker-образов: пользователь вставляет их как
// получилось — по строкам, через запятую, точку с запятой или пробел
// (ни один из этих символов в image-ссылке не встречается). Порядок
// сохраняется, дубли схлопываются.
const separators = /[\s,;]+/
const bullet = /^[-*•]+$/

export function parseImageList(raw: string): string[] {
  const result: string[] = []
  const seen = new Set<string>()

  for (const token of raw.split(separators)) {
    // кавычки и хвостовая пунктуация остаются от копирования из yaml/списков
    const image = token.replace(/^["'`]+/, '').replace(/["'`,;]+$/, '')
    if (!image || bullet.test(image) || seen.has(image))
      continue
    seen.add(image)
    result.push(image)
  }

  return result
}
