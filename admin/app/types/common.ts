// Общие типы gateway API (grpc-gateway, protojson: snake_case,
// int64 приходят строками, timestamp — RFC3339-строками).

// type-алиас (не interface): алиасу TS даёт неявную индексную сигнатуру,
// и он проходит в QueryParams api-клиента без кастов
export type ListParams = {
  page?: number
  page_size?: number
  with_total_count?: boolean
  only_count?: boolean
  sort?: string[]
}

export interface PaginationInfo {
  page: string
  page_size: string
  total_count: string
}

export interface ErrorRep {
  code: string
  message: string
  fields?: Record<string, string>
}

// Составной идентификатор дага: проект (docker-образ) + имя инстанса.
export interface DagRef {
  project: string
  name: string
}

// Скоуп значения (переменной, секрета, настройки): пустые оба поля —
// глобальный, только project — проектный, оба — скоуп дага. Более узкий
// перекрывает более широкий при резолве в момент запуска.
export interface Scope {
  project: string
  dag: string
}
